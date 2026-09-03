package verify_test

import (
	"context"
	"testing"

	"github.com/douglasjarquin/made/internal/managed"
	"github.com/douglasjarquin/made/internal/verify"
)

func stageStatus(t *testing.T, res verify.EngineResult, stage string) managed.Outcome {
	t.Helper()
	for _, s := range res.StageResults {
		if s.Stage == stage {
			return s.Outcome
		}
	}
	t.Fatalf("stage %q not present in results %+v", stage, res.StageResults)
	return ""
}

func TestRunEngine_TestAndLintOnly(t *testing.T) {
	dir, baseSHA, inputSHA := newTestRepo(t, ".made.yaml", testConfigNoAgent)
	rc, err := verify.ResolveContext(context.Background(), dir, "origin/main")
	if err != nil {
		t.Fatalf("ResolveContext: %v", err)
	}

	res, err := verify.RunEngine(context.Background(), verify.EngineParams{
		Workspace:    rc.Repository.Root,
		BaseSHA:      baseSHA,
		InputSHA:     inputSHA,
		ConfigPath:   rc.Config.Path,
		PolicyHash:   rc.Config.Hash,
		ReviewSource: managed.ReviewSourceInternal,
		RunID:        "verify-test",
		MissionID:    "made-verify",
		StateDir:     verify.StateRoot(rc.Repository.Root),
	})
	if err != nil {
		t.Fatalf("RunEngine: %v", err)
	}
	if res.Outcome != managed.OutcomePassed {
		t.Fatalf("Outcome = %q, want passed (message=%q)", res.Outcome, res.Message)
	}
	if got := stageStatus(t, res, "review"); got != managed.OutcomeNotConfigured {
		t.Errorf("review stage = %q, want not_configured", got)
	}
	if got := stageStatus(t, res, "test"); got != managed.OutcomePassed {
		t.Errorf("test stage = %q, want passed", got)
	}
	if got := stageStatus(t, res, "document"); got != managed.OutcomeNotConfigured {
		t.Errorf("document stage = %q, want not_configured", got)
	}
	if got := stageStatus(t, res, "lint"); got != managed.OutcomePassed {
		t.Errorf("lint stage = %q, want passed", got)
	}
}

func TestRunEngine_NoEffectiveWorkIsInfrastructureError(t *testing.T) {
	dir, baseSHA, inputSHA := newTestRepo(t, "", "")
	rc, err := verify.ResolveContext(context.Background(), dir, "origin/main")
	if err != nil {
		t.Fatalf("ResolveContext: %v", err)
	}

	res, err := verify.RunEngine(context.Background(), verify.EngineParams{
		Workspace:    rc.Repository.Root,
		BaseSHA:      baseSHA,
		InputSHA:     inputSHA,
		ConfigPath:   rc.Config.Path,
		PolicyHash:   rc.Config.Hash,
		ReviewSource: managed.ReviewSourceInternal,
		RunID:        "verify-test",
		MissionID:    "made-verify",
		StateDir:     verify.StateRoot(rc.Repository.Root),
	})
	if err != nil {
		t.Fatalf("RunEngine: %v", err)
	}
	if res.Outcome != managed.OutcomeInfrastructureError {
		t.Fatalf("Outcome = %q, want infrastructure_error", res.Outcome)
	}
}

func TestRunEngine_DisabledStageReportsDisabled(t *testing.T) {
	cfg := "version: 1\nstages:\n  lint:\n    enabled: false\ncommands:\n  test: \"true\"\n  lint: \"true\"\n"
	dir, baseSHA, inputSHA := newTestRepo(t, ".made.yaml", cfg)
	rc, err := verify.ResolveContext(context.Background(), dir, "origin/main")
	if err != nil {
		t.Fatalf("ResolveContext: %v", err)
	}

	res, err := verify.RunEngine(context.Background(), verify.EngineParams{
		Workspace:    rc.Repository.Root,
		BaseSHA:      baseSHA,
		InputSHA:     inputSHA,
		ConfigPath:   rc.Config.Path,
		PolicyHash:   rc.Config.Hash,
		ReviewSource: managed.ReviewSourceInternal,
		RunID:        "verify-test",
		MissionID:    "made-verify",
		StateDir:     verify.StateRoot(rc.Repository.Root),
	})
	if err != nil {
		t.Fatalf("RunEngine: %v", err)
	}
	if res.Outcome != managed.OutcomePassed {
		t.Fatalf("Outcome = %q, want passed (message=%q)", res.Outcome, res.Message)
	}
	if got := stageStatus(t, res, "lint"); got != managed.OutcomeDisabled {
		t.Errorf("lint stage = %q, want disabled", got)
	}
}

func TestRunEngine_FailingTestIsFailedRetryable(t *testing.T) {
	dir, baseSHA, inputSHA := newTestRepo(t, ".made.yaml", testConfigFailingTest)
	rc, err := verify.ResolveContext(context.Background(), dir, "origin/main")
	if err != nil {
		t.Fatalf("ResolveContext: %v", err)
	}

	res, err := verify.RunEngine(context.Background(), verify.EngineParams{
		Workspace:    rc.Repository.Root,
		BaseSHA:      baseSHA,
		InputSHA:     inputSHA,
		ConfigPath:   rc.Config.Path,
		PolicyHash:   rc.Config.Hash,
		ReviewSource: managed.ReviewSourceInternal,
		RunID:        "verify-test",
		MissionID:    "made-verify",
		StateDir:     verify.StateRoot(rc.Repository.Root),
	})
	if err != nil {
		t.Fatalf("RunEngine: %v", err)
	}
	if res.Outcome != managed.OutcomeFailedRetryable {
		t.Fatalf("Outcome = %q, want failed_retryable", res.Outcome)
	}
}

func TestRunEngine_ReviewRequiredWithoutAgentUsesExternalSource(t *testing.T) {
	dir, baseSHA, inputSHA := newTestRepo(t, ".made.yaml", testConfigReviewRequired)
	rc, err := verify.ResolveContext(context.Background(), dir, "origin/main")
	if err != nil {
		t.Fatalf("ResolveContext: %v", err)
	}

	res, err := verify.RunEngine(context.Background(), verify.EngineParams{
		Workspace:    rc.Repository.Root,
		BaseSHA:      baseSHA,
		InputSHA:     inputSHA,
		ConfigPath:   rc.Config.Path,
		PolicyHash:   rc.Config.Hash,
		ReviewSource: managed.ReviewSourceInternal,
		RunID:        "verify-test",
		MissionID:    "made-verify",
		StateDir:     verify.StateRoot(rc.Repository.Root),
	})
	if err != nil {
		t.Fatalf("RunEngine: %v", err)
	}
	// No agent configured and review-source=internal: internal review cannot
	// be satisfied, so the stage must surface a real failure - never a
	// silent pass and never a fabricated "not_configured".
	if got := stageStatus(t, res, "review"); got == managed.OutcomeNotConfigured || got == managed.OutcomePassed {
		t.Errorf("review stage = %q, want a genuine failure since no agent is configured for an internal review", got)
	}
}
