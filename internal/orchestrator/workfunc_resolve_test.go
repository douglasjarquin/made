package orchestrator

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
	"github.com/douglasjarquin/made/internal/gitgate"
	"github.com/douglasjarquin/made/internal/pipeline/review"
)

// stubResolveProbes puts a trivial "exit 0" script on a scoped PATH for each
// kind, so agent.Resolve's presence and auth steps both pass for it - a
// real review.Run spawn always goes through bubblewrap containment, which
// spawnReview's fallback-loop logic under test here does not need to
// exercise (that's covered by internal/agent's own resolve_test.go).
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

// stubReviewRun replaces the reviewRun seam with a fixed call-by-call
// sequence, so spawnReview's retry loop can be exercised without a real
// agent spawn.
func stubReviewRun(t *testing.T, calls ...func(kind agent.Kind) (review.Result, error)) {
	t.Helper()
	original := reviewRun
	idx := 0
	reviewRun = func(_ context.Context, _ string, kind agent.Kind, _ review.Options) (review.Result, error) {
		if idx >= len(calls) {
			t.Fatalf("reviewRun called more times (%d) than stubbed (%d)", idx+1, len(calls))
		}
		fn := calls[idx]
		idx++
		return fn(kind)
	}
	t.Cleanup(func() {
		reviewRun = original
		if idx != len(calls) {
			t.Errorf("reviewRun called %d time(s), want exactly %d", idx, len(calls))
		}
	})
}

func testChain(cfg config.Config) *chain {
	return &chain{ctx: context.Background(), rc: &RunContext{Config: cfg, Worktree: &gitgate.Worktree{Path: "/fake/worktree"}}}
}

func TestSpawnReview_PinnedAgentUnaffected(t *testing.T) {
	stubReviewRun(t, func(kind agent.Kind) (review.Result, error) {
		if kind != agent.KindClaude {
			t.Errorf("reviewRun kind = %v, want claude", kind)
		}
		return review.Result{OK: true, Message: "ok"}, nil
	})

	c := testChain(config.Config{Agent: "claude"})
	result, err := c.spawnReview(review.Options{})

	if err != nil {
		t.Fatalf("spawnReview() error = %v", err)
	}
	if result == nil || !result.OK {
		t.Fatalf("spawnReview() result = %+v, want OK", result)
	}
	if c.reviewAgentResolution != nil {
		t.Errorf("reviewAgentResolution = %+v, want nil on the pinned fast path", c.reviewAgentResolution)
	}
}

func TestSpawnReview_MidRunCapacityFallbackSucceeds(t *testing.T) {
	stubResolveProbes(t, agent.KindClaude, agent.KindCodex)
	stubReviewRun(t,
		func(kind agent.Kind) (review.Result, error) {
			if kind != agent.KindClaude {
				t.Errorf("first reviewRun kind = %v, want claude", kind)
			}
			return review.Result{}, fmt.Errorf("%w: claude usage limit reached", agent.ErrAgentCapacity)
		},
		func(kind agent.Kind) (review.Result, error) {
			if kind != agent.KindCodex {
				t.Errorf("second reviewRun kind = %v, want codex", kind)
			}
			return review.Result{OK: true, Message: "ok"}, nil
		},
	)

	c := testChain(config.Config{Agent: "auto", Agents: []string{"claude", "codex"}})
	result, err := c.spawnReview(review.Options{})

	if err != nil {
		t.Fatalf("spawnReview() error = %v", err)
	}
	if result == nil || !result.OK {
		t.Fatalf("spawnReview() result = %+v, want OK (fell back to codex)", result)
	}
	if c.reviewAgentResolution == nil || c.reviewAgentResolution.Selected == nil || *c.reviewAgentResolution.Selected != agent.KindCodex {
		t.Fatalf("reviewAgentResolution.Selected = %v, want codex", c.reviewAgentResolution)
	}
	if len(c.reviewAgentResolution.Attempts) != 1 || c.reviewAgentResolution.Attempts[0].Kind != agent.KindClaude || c.reviewAgentResolution.Attempts[0].Reason != agent.ReasonQuotaExhausted {
		t.Errorf("reviewAgentResolution.Attempts = %+v, want one quota_exhausted attempt for claude", c.reviewAgentResolution.Attempts)
	}
}

func TestSpawnReview_NonCapacityFailureIsNotRetried(t *testing.T) {
	stubResolveProbes(t, agent.KindClaude, agent.KindCodex)
	wantErr := errors.New("review: agent modified worktree")
	stubReviewRun(t, func(kind agent.Kind) (review.Result, error) {
		if kind != agent.KindClaude {
			t.Errorf("reviewRun kind = %v, want claude", kind)
		}
		return review.Result{}, wantErr
	})

	c := testChain(config.Config{Agent: "auto", Agents: []string{"claude", "codex"}})
	result, err := c.spawnReview(review.Options{})

	if result != nil {
		t.Errorf("spawnReview() result = %+v, want nil", result)
	}
	if !errors.Is(err, wantErr) {
		t.Fatalf("spawnReview() error = %v, want %v (no retry on a non-capacity failure)", err, wantErr)
	}
}

func TestSpawnReview_AllCandidatesExhaustedNeverCallsReviewRun(t *testing.T) {
	stubReviewRun(t) // zero calls expected: both candidates fail auth before any spawn is attempted.

	dir := t.TempDir()
	for _, kind := range []agent.Kind{agent.KindClaude, agent.KindCodex} {
		path := filepath.Join(dir, kind.BinaryName())
		if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
			t.Fatalf("write stub binary %s: %v", path, err)
		}
	}
	t.Setenv("PATH", dir)

	c := testChain(config.Config{Agent: "auto", Agents: []string{"claude", "codex"}})
	result, err := c.spawnReview(review.Options{})

	if err != nil {
		t.Fatalf("spawnReview() error = %v, want nil (exhaustion is not an error return)", err)
	}
	if result != nil {
		t.Errorf("spawnReview() result = %+v, want nil", result)
	}
	if c.reviewAgentResolution == nil || !c.reviewAgentResolution.AllExhausted() {
		t.Fatalf("reviewAgentResolution = %+v, want AllExhausted()", c.reviewAgentResolution)
	}
	if len(c.reviewAgentResolution.Attempts) != 2 {
		t.Errorf("reviewAgentResolution.Attempts = %+v, want 2 entries", c.reviewAgentResolution.Attempts)
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
