package verify_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/douglasjarquin/made/internal/config"
	"github.com/douglasjarquin/made/internal/managed"
	"github.com/douglasjarquin/made/internal/planner"
	"github.com/douglasjarquin/made/internal/receipt"
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

func TestRunEngine_EvidenceDirIsPerInvocationNotRoot(t *testing.T) {
	dir, baseSHA, inputSHA := newTestRepo(t, ".made.yaml", testConfigNoAgent)
	rc, err := verify.ResolveContext(context.Background(), dir, "origin/main")
	if err != nil {
		t.Fatalf("ResolveContext: %v", err)
	}

	stateDir := verify.StateRoot(rc.Repository.Root)
	res, err := verify.RunEngine(context.Background(), verify.EngineParams{
		Workspace:    rc.Repository.Root,
		BaseSHA:      baseSHA,
		InputSHA:     inputSHA,
		ConfigPath:   rc.Config.Path,
		PolicyHash:   rc.Config.Hash,
		ReviewSource: managed.ReviewSourceInternal,
		RunID:        "verify-test",
		MissionID:    "made-verify",
		StateDir:     stateDir,
	})
	if err != nil {
		t.Fatalf("RunEngine: %v", err)
	}

	if res.EvidenceDir == verify.EvidenceRoot(stateDir) {
		t.Fatalf("EvidenceDir = %q, want the per-invocation directory, not the shared evidence root", res.EvidenceDir)
	}
	terminalPath := filepath.Join(res.EvidenceDir, "terminal.json")
	if _, err := os.Stat(terminalPath); err != nil {
		t.Errorf("expected terminal.json under EvidenceDir %q: %v", res.EvidenceDir, err)
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

func TestRunEngine_ReusesLaneCommandFromPublishedReceipt(t *testing.T) {
	dir, baseSHA, inputSHA := newTestRepo(t, ".made.yaml", testConfigLaneGo)
	remoteParent := t.TempDir()
	remote := filepath.Join(remoteParent, "remote.git")
	if out, err := exec.Command("git", "-C", remoteParent, "init", "-q", "--bare", remote).CombinedOutput(); err != nil {
		t.Fatalf("git init --bare: %v: %s", err, out)
	}
	if out, err := exec.Command("git", "-C", dir, "remote", "add", "origin", remote).CombinedOutput(); err != nil {
		t.Fatalf("git remote add origin: %v: %s", err, out)
	}

	rc, err := verify.ResolveContext(context.Background(), dir, "origin/main")
	if err != nil {
		t.Fatalf("ResolveContext: %v", err)
	}

	configBytes, err := os.ReadFile(rc.Config.Path)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	cfg, err := config.ParseBytes(configBytes)
	if err != nil {
		t.Fatalf("parse config: %v", err)
	}
	changedPaths := []string{"hello.go"}
	decisions, err := planner.SelectLanes(cfg.Validation.Lanes, changedPaths)
	if err != nil {
		t.Fatalf("SelectLanes: %v", err)
	}
	configHash, err := planner.HashConfig(cfg)
	if err != nil {
		t.Fatalf("HashConfig: %v", err)
	}
	var matchedPaths []string
	for _, d := range decisions {
		if d.Name == "go" {
			matchedPaths = d.MatchedPaths
		}
	}
	fp := receipt.BuildLaneFingerprint(receipt.LaneFingerprintInputs{
		RepoIdentity: receipt.RepoIdentity(context.Background(), dir),
		BaseSHA:      baseSHA,
		CandidateSHA: inputSHA,
		ConfigHash:   configHash,
		LaneName:     "go",
		MatchedPaths: matchedPaths,
		Command:      cfg.Validation.Lanes["go"].FullShellCommands()[0],
		MadeVersion:  managed.MadeVersion,
	})
	store := &receipt.Store{RepoPath: dir}
	now := time.Now().UTC()
	if _, err := store.Put(context.Background(), fp.Hash(), receipt.Receipt{
		SchemaVersion: receipt.ReceiptSchemaVersion,
		Fingerprint:   fp,
		SourceRunID:   "prior-run",
		StartedAt:     now,
		CompletedAt:   now,
		MadeVersion:   managed.MadeVersion,
	}); err != nil {
		t.Fatalf("Put: %v", err)
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

	receiptOut := verify.BuildReceipt(dir, baseSHA, inputSHA, verify.ConfigIdentity{}, nil, res)
	var testStage verify.StageReceipt
	found := false
	for _, s := range receiptOut.Stages {
		if s.Name == "test" {
			testStage = s
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a test stage in the receipt, got %+v", receiptOut.Stages)
	}
	if len(testStage.Reused) != 1 || testStage.Reused[0].Name != "go" || testStage.Reused[0].SourceRunID != "prior-run" {
		t.Fatalf("expected the go lane to be surfaced as reused on the receipt, got %+v", testStage)
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
