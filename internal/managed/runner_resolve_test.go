package managed

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/douglasjarquin/made/internal/agent"
	"github.com/douglasjarquin/made/internal/config"
)

// stubResolveProbes puts a trivial "exit 0" script on a scoped PATH for each
// kind, so agent.Resolve's presence and auth steps both pass for it,
// without needing a real (bubblewrap-contained) agent spawn - that path is
// covered by internal/agent's own resolve_test.go.
func stubResolveProbes(t *testing.T, kinds ...agent.Kind) {
	t.Helper()
	dir := t.TempDir()
	for _, kind := range kinds {
		path := filepath.Join(dir, kind.BinaryName())
		if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
			t.Fatalf("write stub binary %s: %v", path, err)
		}
	}
	t.Setenv("PATH", dir)
}

// fakeReviewSource returns a fixed call-by-call sequence of (ReviewResult,
// error) pairs regardless of the requested AgentKind, so
// spawnInternalReview's retry loop can be exercised without a real agent
// spawn.
type fakeReviewSource struct {
	t     *testing.T
	calls []func(kind agent.Kind) (ReviewResult, error)
	idx   int
}

func (f *fakeReviewSource) Review(_ context.Context, req ReviewRequest) (ReviewResult, error) {
	f.t.Helper()
	if f.idx >= len(f.calls) {
		f.t.Fatalf("Review called more times (%d) than stubbed (%d)", f.idx+1, len(f.calls))
	}
	fn := f.calls[f.idx]
	f.idx++
	return fn(req.AgentKind)
}

func (f *fakeReviewSource) assertAllCalled() {
	f.t.Helper()
	if f.idx != len(f.calls) {
		f.t.Errorf("Review called %d time(s), want exactly %d", f.idx, len(f.calls))
	}
}

func TestSpawnInternalReview_PinnedAgentUnaffected(t *testing.T) {
	source := &fakeReviewSource{t: t, calls: []func(agent.Kind) (ReviewResult, error){
		func(kind agent.Kind) (ReviewResult, error) {
			if kind != agent.KindClaude {
				t.Errorf("Review kind = %v, want claude", kind)
			}
			return ReviewResult{OK: true, Message: "ok"}, nil
		},
	}}
	defer source.assertAllCalled()

	r := &Runner{cfg: config.Config{Agent: "claude"}}
	result, err := r.spawnInternalReview(context.Background(), source, ReviewRequest{})

	if err != nil {
		t.Fatalf("spawnInternalReview() error = %v", err)
	}
	if result == nil || !result.OK {
		t.Fatalf("spawnInternalReview() result = %+v, want OK", result)
	}
	if r.reviewAgentResolution != nil {
		t.Errorf("reviewAgentResolution = %+v, want nil on the pinned fast path", r.reviewAgentResolution)
	}
}

func TestSpawnInternalReview_MidRunCapacityFallbackSucceeds(t *testing.T) {
	stubResolveProbes(t, agent.KindClaude, agent.KindCodex)
	source := &fakeReviewSource{t: t, calls: []func(agent.Kind) (ReviewResult, error){
		func(kind agent.Kind) (ReviewResult, error) {
			if kind != agent.KindClaude {
				t.Errorf("first Review kind = %v, want claude", kind)
			}
			return ReviewResult{}, fmt.Errorf("%w: claude usage limit reached", agent.ErrAgentCapacity)
		},
		func(kind agent.Kind) (ReviewResult, error) {
			if kind != agent.KindCodex {
				t.Errorf("second Review kind = %v, want codex", kind)
			}
			return ReviewResult{OK: true, Message: "ok"}, nil
		},
	}}
	defer source.assertAllCalled()

	r := &Runner{cfg: config.Config{Agent: "auto", Agents: []string{"claude", "codex"}}}
	result, err := r.spawnInternalReview(context.Background(), source, ReviewRequest{})

	if err != nil {
		t.Fatalf("spawnInternalReview() error = %v", err)
	}
	if result == nil || !result.OK {
		t.Fatalf("spawnInternalReview() result = %+v, want OK (fell back to codex)", result)
	}
	if r.reviewAgentResolution == nil || r.reviewAgentResolution.Selected == nil || *r.reviewAgentResolution.Selected != agent.KindCodex {
		t.Fatalf("reviewAgentResolution.Selected = %v, want codex", r.reviewAgentResolution)
	}
	if len(r.reviewAgentResolution.Attempts) != 1 || r.reviewAgentResolution.Attempts[0].Kind != agent.KindClaude || r.reviewAgentResolution.Attempts[0].Reason != agent.ReasonQuotaExhausted {
		t.Errorf("reviewAgentResolution.Attempts = %+v, want one quota_exhausted attempt for claude", r.reviewAgentResolution.Attempts)
	}
}

func TestSpawnInternalReview_NonCapacityFailureIsNotRetried(t *testing.T) {
	stubResolveProbes(t, agent.KindClaude, agent.KindCodex)
	wantErr := errors.New("review: agent modified worktree")
	source := &fakeReviewSource{t: t, calls: []func(agent.Kind) (ReviewResult, error){
		func(kind agent.Kind) (ReviewResult, error) {
			if kind != agent.KindClaude {
				t.Errorf("Review kind = %v, want claude", kind)
			}
			return ReviewResult{}, wantErr
		},
	}}
	defer source.assertAllCalled()

	r := &Runner{cfg: config.Config{Agent: "auto", Agents: []string{"claude", "codex"}}}
	result, err := r.spawnInternalReview(context.Background(), source, ReviewRequest{})

	if result != nil {
		t.Errorf("spawnInternalReview() result = %+v, want nil", result)
	}
	if !errors.Is(err, wantErr) {
		t.Fatalf("spawnInternalReview() error = %v, want %v (no retry on a non-capacity failure)", err, wantErr)
	}
}

func TestSpawnInternalReview_AllCandidatesExhaustedNeverCallsReview(t *testing.T) {
	source := &fakeReviewSource{t: t} // zero calls expected: both candidates fail auth before any spawn is attempted.
	defer source.assertAllCalled()

	dir := t.TempDir()
	for _, kind := range []agent.Kind{agent.KindClaude, agent.KindCodex} {
		path := filepath.Join(dir, kind.BinaryName())
		if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
			t.Fatalf("write stub binary %s: %v", path, err)
		}
	}
	t.Setenv("PATH", dir)

	r := &Runner{cfg: config.Config{Agent: "auto", Agents: []string{"claude", "codex"}}}
	result, err := r.spawnInternalReview(context.Background(), source, ReviewRequest{})

	if err != nil {
		t.Fatalf("spawnInternalReview() error = %v, want nil (exhaustion is not an error return)", err)
	}
	if result != nil {
		t.Errorf("spawnInternalReview() result = %+v, want nil", result)
	}
	if r.reviewAgentResolution == nil || !r.reviewAgentResolution.AllExhausted() {
		t.Fatalf("reviewAgentResolution = %+v, want AllExhausted()", r.reviewAgentResolution)
	}
	if len(r.reviewAgentResolution.Attempts) != 2 {
		t.Errorf("reviewAgentResolution.Attempts = %+v, want 2 entries", r.reviewAgentResolution.Attempts)
	}
}

func TestFormatAgentResolutionFailure_NamesEveryAttemptAndReason(t *testing.T) {
	res := agent.AgentResolution{
		Attempts: []agent.CandidateAttempt{
			{Kind: agent.KindCodex, Reason: agent.ReasonMissing},
			{Kind: agent.KindClaude, Reason: agent.ReasonUnauthenticated},
		},
	}
	msg := formatAgentResolutionFailure(res)
	if !strings.Contains(msg, "codex") || !strings.Contains(msg, "missing") || !strings.Contains(msg, "claude") || !strings.Contains(msg, "unauthenticated") {
		t.Errorf("formatAgentResolutionFailure() = %q, want it to name every candidate and reason", msg)
	}
}
