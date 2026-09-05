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
	"github.com/douglasjarquin/made/internal/daemon"
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

func TestFinish_ReviewStageSurfacesAgentResolutionOnStageResult(t *testing.T) {
	rm := daemon.NewRunManager()
	runID := rm.NewRunID()
	if _, err := rm.Submit(runID, "repo", "branch", func(context.Context, func(daemon.Event)) error { return nil }); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	claude := agent.KindClaude
	c := &chain{rm: rm, runID: runID, reviewAgentResolution: &agent.AgentResolution{
		Selected: &claude,
		Attempts: []agent.CandidateAttempt{{Kind: agent.KindCodex, Reason: agent.ReasonMissing}},
	}}

	if err := c.finish(stageNameReview, stageResultPass, "ok"); err != nil {
		t.Fatalf("finish() error = %v", err)
	}

	snap, ok := rm.Snapshot(runID)
	if !ok {
		t.Fatalf("Snapshot(%q) not found", runID)
	}
	var reviewResult *daemon.StageResult
	for i := range snap.Stages {
		if snap.Stages[i].Name == stageNameReview {
			reviewResult = &snap.Stages[i]
		}
	}
	if reviewResult == nil {
		t.Fatalf("no review StageResult found in %+v", snap.Stages)
	}
	if reviewResult.AgentResolution == nil || reviewResult.AgentResolution.Selected == nil || *reviewResult.AgentResolution.Selected != agent.KindClaude {
		t.Errorf("review StageResult.AgentResolution = %+v, want Selected=claude", reviewResult.AgentResolution)
	}
}

func TestFinish_NonReviewStageNeverCarriesAgentResolution(t *testing.T) {
	rm := daemon.NewRunManager()
	runID := rm.NewRunID()
	if _, err := rm.Submit(runID, "repo", "branch", func(context.Context, func(daemon.Event)) error { return nil }); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	claude := agent.KindClaude
	c := &chain{rm: rm, runID: runID, reviewAgentResolution: &agent.AgentResolution{Selected: &claude}}

	if err := c.finish(stageNameTest, stageResultPass, "ok"); err != nil {
		t.Fatalf("finish() error = %v", err)
	}

	snap, ok := rm.Snapshot(runID)
	if !ok {
		t.Fatalf("Snapshot(%q) not found", runID)
	}
	for _, s := range snap.Stages {
		if s.Name == stageNameTest && s.AgentResolution != nil {
			t.Errorf("test StageResult.AgentResolution = %+v, want nil (only the review stage carries this)", s.AgentResolution)
		}
	}
}

func TestSpawnReview_PushOptionPreferenceHonoredWhenRepoCommandsAllowed(t *testing.T) {
	stubReviewRun(t, func(kind agent.Kind) (review.Result, error) {
		if kind != agent.KindClaude {
			t.Errorf("reviewRun kind = %v, want claude (from the push-option preference)", kind)
		}
		return review.Result{OK: true, Message: "ok"}, nil
	})

	c := testChain(config.Config{Agent: "auto", Agents: []string{"codex"}, AllowRepoCommands: true})
	c.opts.AgentPreference = "claude"
	result, err := c.spawnReview(review.Options{})

	if err != nil {
		t.Fatalf("spawnReview() error = %v", err)
	}
	if result == nil || !result.OK {
		t.Fatalf("spawnReview() result = %+v, want OK using the preference, not config's codex", result)
	}
	if c.reviewAgentResolution != nil {
		t.Errorf("reviewAgentResolution = %+v, want nil (preference fast path never probes)", c.reviewAgentResolution)
	}
}

func TestSpawnReview_PushOptionPreferenceIgnoredWhenRepoCommandsDisallowed(t *testing.T) {
	stubResolveProbes(t, agent.KindCodex)
	stubReviewRun(t, func(kind agent.Kind) (review.Result, error) {
		if kind != agent.KindCodex {
			t.Errorf("reviewRun kind = %v, want codex (config's own resolution, preference must be ignored)", kind)
		}
		return review.Result{OK: true, Message: "ok"}, nil
	})

	c := testChain(config.Config{Agent: "auto", Agents: []string{"codex"}, AllowRepoCommands: false})
	c.opts.AgentPreference = "claude"
	result, err := c.spawnReview(review.Options{})

	if err != nil {
		t.Fatalf("spawnReview() error = %v", err)
	}
	if result == nil || !result.OK {
		t.Fatalf("spawnReview() result = %+v, want OK using codex (config), ignoring the preference", result)
	}
}
