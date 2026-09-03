package managed_test

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/douglasjarquin/made/internal/managed"
)

// TestRunner_TestStage_AllLaneCommandsReused_SkipsExecutionAndPasses proves
// the "everything reused" edge case: with no commands.test configured and
// every selected lane's Full command satisfied by a published receipt,
// TestExtras is empty but the Test stage is still planned to run (it has
// real, receipt-backed coverage) - testStage must pass without invoking
// test.Run's "no test command configured" guard, and without creating any
// evidence for a command that never actually executed.
func TestRunner_TestStage_AllLaneCommandsReused_SkipsExecutionAndPasses(t *testing.T) {
	dir, baseSHA, inputSHA := laneReuseTestRepo(t)
	cfg := laneReuseConfig()
	reuse := managed.LaneReuseContext{Workspace: dir, BaseSHA: baseSHA, CandidateSHA: inputSHA}
	changedPaths := []string{"hello.go"}
	publishLaneReuseReceipt(t, cfg, reuse, changedPaths, "prior-run")

	plan, err := managed.BuildStagePlan(context.Background(), cfg, changedPaths, managed.ReviewSourceInternal, reuse)
	if err != nil {
		t.Fatalf("BuildStagePlan: %v", err)
	}
	if len(plan.Test.TestExtras) != 0 || len(plan.Test.TestReused) != 1 {
		t.Fatalf("test fixture assumption broken: %+v", plan.Test)
	}
	if plan.Test.State != managed.StagePlanRun {
		t.Fatalf("expected the test stage to still be planned to run, got %s", plan.Test.State)
	}

	opts := &managed.Options{
		RunID:       "run-all-reused",
		MissionID:   "mission-all-reused",
		Workspace:   dir,
		BaseSHA:     baseSHA,
		InputSHA:    inputSHA,
		EvidenceDir: t.TempDir(),
	}
	ew := managed.NewEventWriter(io.Discard, opts)
	ev := managed.NewManagedEvidenceStore(opts.EvidenceDir, opts.RunID, "invocation-all-reused")
	decisions, err := managed.LoadDecisions("", opts)
	if err != nil {
		t.Fatalf("LoadDecisions: %v", err)
	}

	runner := managed.NewRunner(opts, cfg, ew, ev, decisions, plan, nil)
	outcome, msg, stoppedAt := runner.Run(context.Background())
	if outcome != managed.OutcomePassed {
		t.Fatalf("outcome = %s (stopped at %s), want passed; message=%q", outcome, stoppedAt, msg)
	}

	var testResult managed.StageResult
	found := false
	for _, r := range runner.StageResults() {
		if r.Stage == "test" {
			testResult = r
			found = true
		}
	}
	if !found || testResult.Outcome != managed.OutcomePassed {
		t.Fatalf("expected the test stage to be present and passed, got %+v (present=%v)", testResult, found)
	}
	if len(testResult.ReusedCommands) != 1 || testResult.ReusedCommands[0].Name != "go" {
		t.Fatalf("expected the reused go lane to be surfaced on the test stage result, got %+v", testResult.ReusedCommands)
	}

	entries, err := os.ReadDir(ev.StageDir("test"))
	if err == nil {
		for _, e := range entries {
			if filepath.Base(e.Name()) == "go-stdout.log" || filepath.Base(e.Name()) == "go-stderr.log" {
				t.Fatalf("expected no evidence for a command that was reused, not executed, but found %s", e.Name())
			}
		}
	}
}
