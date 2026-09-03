package managed_test

import (
	"context"
	"io"
	"testing"

	"github.com/douglasjarquin/made/internal/agent"
	"github.com/douglasjarquin/made/internal/config"
	"github.com/douglasjarquin/made/internal/managed"
)

// TestRunner_UnreachedRunStagesStayPending reproduces the real canary
// receipt bug: Review, Test, and Lint are all planned to run, Review fails
// terminally, and Test/Lint must still appear in StageResults at pending
// instead of vanishing from the result set.
func TestRunner_UnreachedRunStagesStayPending(t *testing.T) {
	opts := e2eOptions(t, "G-pending", "M-pending", agent.Findings{Findings: []agent.Finding{
		finding(agent.FindingBlocking, "sec.sqli", "security", "SQL injection", "feature.go"),
	}})

	cfg, err := config.ParseBytes([]byte(agentConfig))
	if err != nil {
		t.Fatalf("parse config: %v", err)
	}
	plan, err := managed.BuildStagePlan(context.Background(), cfg, nil, managed.ReviewSourceInternal, managed.LaneReuseContext{})
	if err != nil {
		t.Fatalf("build stage plan: %v", err)
	}
	for name, state := range map[string]managed.StagePlanState{
		"review": plan.Review.State,
		"test":   plan.Test.State,
		"lint":   plan.Lint.State,
	} {
		if state != managed.StagePlanRun {
			t.Fatalf("expected stage %s planned to run, got %s", name, state)
		}
	}

	ew := managed.NewEventWriter(io.Discard, opts)
	ev := managed.NewManagedEvidenceStore(opts.EvidenceDir, opts.RunID, "test-invocation")
	decisions, err := managed.LoadDecisions("", opts)
	if err != nil {
		t.Fatalf("load decisions: %v", err)
	}

	runner := managed.NewRunner(opts, cfg, ew, ev, decisions, plan, nil)
	outcome, _, _ := runner.Run(context.Background())
	if outcome != managed.OutcomeFailedTerminal {
		t.Fatalf("expected outcome failed_terminal, got %s", outcome)
	}

	results := make(map[string]managed.StageResult)
	for _, r := range runner.StageResults() {
		results[r.Stage] = r
	}

	if got, ok := results["review"]; !ok || got.Outcome != managed.OutcomeFailedTerminal {
		t.Errorf("expected review stage failed_terminal and present, got %+v (present=%v)", got, ok)
	}
	if got, ok := results["test"]; !ok || got.Outcome != managed.OutcomePending {
		t.Errorf("expected test stage pending and present, got %+v (present=%v)", got, ok)
	}
	if got, ok := results["lint"]; !ok || got.Outcome != managed.OutcomePending {
		t.Errorf("expected lint stage pending and present, got %+v (present=%v)", got, ok)
	}
}
