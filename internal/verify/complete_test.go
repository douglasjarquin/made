package verify_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/douglasjarquin/made/internal/managed"
	"github.com/douglasjarquin/made/internal/verify"
)

func prepareRequest(t *testing.T, dir string) (verify.PrepareOutcome, string) {
	t.Helper()
	requestPath := filepath.Join(t.TempDir(), "request.json")
	out, err := verify.Prepare(context.Background(), verify.PrepareParams{
		WorkDir:  dir,
		BaseRef:  "origin/main",
		Executor: "cursor",
		Output:   requestPath,
	})
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	return out, requestPath
}

func writeExternalResult(t *testing.T, req verify.Request, override func(*managed.ExternalReviewResult)) string {
	t.Helper()
	result := managed.ExternalReviewResult{
		SchemaVersion:         managed.ExternalReviewSchemaVersion,
		ReviewContractVersion: managed.ReviewContractVersion,
		Executor:              "cursor-agent",
		Reviewer:              "claude",
		RequestedModel:        req.RequestedModel,
		ActualModel:           "claude-opus-5-actual",
		BaseSHA:               req.Contract.BaseSHA,
		InputSHA:              req.Contract.InputSHA,
		PolicyHash:            req.Config.Hash,
		ReviewContractHash:    req.ContractHash,
		Findings:              []managed.ExternalFinding{},
	}
	if override != nil {
		override(&result)
	}
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "review-result.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestPrepareComplete_ExternalRoundTripPasses(t *testing.T) {
	dir, _, inputSHA := newTestRepo(t, ".made.yaml", testConfigReviewRequired)
	out, requestPath := prepareRequest(t, dir)
	resultPath := writeExternalResult(t, out.Request, nil)

	co, err := verify.Complete(context.Background(), verify.CompleteParams{
		WorkDir:          dir,
		RequestPath:      requestPath,
		ReviewResultPath: resultPath,
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if co.Receipt.Outcome != managed.OutcomePassed {
		t.Fatalf("Outcome = %q, want passed (message=%q)", co.Receipt.Outcome, co.Engine.Message)
	}
	if co.Receipt.InputSHA != inputSHA {
		t.Errorf("Receipt.InputSHA = %q, want %q", co.Receipt.InputSHA, inputSHA)
	}
	if co.Receipt.Review == nil || co.Receipt.Review.ActualModel != "claude-opus-5-actual" {
		t.Errorf("Receipt.Review = %+v, want actual_model recorded", co.Receipt.Review)
	}
	if co.Receipt.Review.Source != managed.ReviewSourceExternal {
		t.Errorf("Receipt.Review.Source = %q, want external", co.Receipt.Review.Source)
	}
}

func TestComplete_RepositoryMismatch(t *testing.T) {
	dir, _, _ := newTestRepo(t, ".made.yaml", testConfigReviewRequired)
	out, requestPath := prepareRequest(t, dir)
	resultPath := writeExternalResult(t, out.Request, nil)

	otherDir, _, _ := newTestRepo(t, ".made.yaml", testConfigReviewRequired)

	_, err := verify.Complete(context.Background(), verify.CompleteParams{
		WorkDir:          otherDir,
		RequestPath:      requestPath,
		ReviewResultPath: resultPath,
	})
	if err == nil || !strings.Contains(err.Error(), "repository mismatch") {
		t.Fatalf("expected a repository mismatch error, got %v", err)
	}
}

func TestComplete_StaleHeadAfterPrepare(t *testing.T) {
	dir, _, _ := newTestRepo(t, ".made.yaml", testConfigReviewRequired)
	out, requestPath := prepareRequest(t, dir)
	resultPath := writeExternalResult(t, out.Request, nil)

	writeTestFile(t, filepath.Join(dir, "more.go"), "package main\n")
	gitAt(t, dir, "add", ".")
	gitAt(t, dir, "commit", "-m", "moved HEAD")

	_, err := verify.Complete(context.Background(), verify.CompleteParams{
		WorkDir:          dir,
		RequestPath:      requestPath,
		ReviewResultPath: resultPath,
	})
	if err == nil || !strings.Contains(err.Error(), "HEAD moved") {
		t.Fatalf("expected a stale-HEAD error, got %v", err)
	}
}

func TestComplete_DirtyWorktreeAfterPrepare(t *testing.T) {
	dir, _, _ := newTestRepo(t, ".made.yaml", testConfigReviewRequired)
	out, requestPath := prepareRequest(t, dir)
	resultPath := writeExternalResult(t, out.Request, nil)

	writeTestFile(t, filepath.Join(dir, "dirty.txt"), "x\n")

	_, err := verify.Complete(context.Background(), verify.CompleteParams{
		WorkDir:          dir,
		RequestPath:      requestPath,
		ReviewResultPath: resultPath,
	})
	if err == nil || !strings.Contains(err.Error(), "worktree changed") {
		t.Fatalf("expected a dirty-worktree error, got %v", err)
	}
}

func TestComplete_ConfigChangedAfterPrepare(t *testing.T) {
	dir, _, _ := newTestRepo(t, ".made.yaml", testConfigReviewRequired)
	out, requestPath := prepareRequest(t, dir)
	resultPath := writeExternalResult(t, out.Request, nil)

	writeTestFile(t, filepath.Join(dir, ".made.yaml"), testConfigReviewRequired+"no_ci: true\n")
	gitAt(t, dir, "add", ".")
	gitAt(t, dir, "commit", "--amend", "--no-edit")

	_, err := verify.Complete(context.Background(), verify.CompleteParams{
		WorkDir:          dir,
		RequestPath:      requestPath,
		ReviewResultPath: resultPath,
	})
	if err == nil {
		t.Fatal("expected an error because HEAD and config both changed")
	}
}

func TestComplete_GuideChangedAfterPrepare(t *testing.T) {
	cfg := "version: 1\nreview:\n  required: true\n  guides:\n    - GUIDE.md\ncommands:\n  test: \"true\"\n"
	dir, _, _ := newTestRepo(t, ".made.yaml", cfg)
	writeTestFile(t, filepath.Join(dir, "GUIDE.md"), "v1\n")
	gitAt(t, dir, "add", ".")
	gitAt(t, dir, "commit", "-m", "add guide")

	out, requestPath := prepareRequest(t, dir)
	resultPath := writeExternalResult(t, out.Request, nil)

	writeTestFile(t, filepath.Join(dir, "GUIDE.md"), "v2\n")

	_, err := verify.Complete(context.Background(), verify.CompleteParams{
		WorkDir:          dir,
		RequestPath:      requestPath,
		ReviewResultPath: resultPath,
	})
	if err == nil || !strings.Contains(err.Error(), "worktree changed") {
		t.Fatalf("expected the dirty-worktree check to catch the guide edit, got %v", err)
	}
}

func TestComplete_MalformedReviewResultIsInfrastructureError(t *testing.T) {
	dir, _, _ := newTestRepo(t, ".made.yaml", testConfigReviewRequired)
	out, requestPath := prepareRequest(t, dir)
	resultPath := writeExternalResult(t, out.Request, func(r *managed.ExternalReviewResult) {
		r.ReviewContractHash = "sha256:0000000000000000000000000000000000000000000000000000000000000000"
	})

	co, err := verify.Complete(context.Background(), verify.CompleteParams{
		WorkDir:          dir,
		RequestPath:      requestPath,
		ReviewResultPath: resultPath,
	})
	if err != nil {
		t.Fatalf("Complete returned a hard error instead of an infrastructure_error outcome: %v", err)
	}
	if co.Receipt.Outcome != managed.OutcomeInfrastructureError {
		t.Fatalf("Outcome = %q, want infrastructure_error", co.Receipt.Outcome)
	}
}

func TestComplete_BlockingFindingIsFailedTerminal(t *testing.T) {
	dir, _, _ := newTestRepo(t, ".made.yaml", testConfigReviewRequired)
	out, requestPath := prepareRequest(t, dir)
	resultPath := writeExternalResult(t, out.Request, func(r *managed.ExternalReviewResult) {
		r.Findings = []managed.ExternalFinding{{
			Kind:        "blocking",
			Description: "sql injection",
			Code:        "SEC001",
			Class:       "security",
			Paths:       []string{"hello.go"},
		}}
	})

	co, err := verify.Complete(context.Background(), verify.CompleteParams{
		WorkDir:          dir,
		RequestPath:      requestPath,
		ReviewResultPath: resultPath,
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if co.Receipt.Outcome != managed.OutcomeFailedTerminal {
		t.Fatalf("Outcome = %q, want failed_terminal", co.Receipt.Outcome)
	}
}

func TestComplete_AskUserFindingNeedsDecision(t *testing.T) {
	dir, _, _ := newTestRepo(t, ".made.yaml", testConfigReviewRequired)
	out, requestPath := prepareRequest(t, dir)
	resultPath := writeExternalResult(t, out.Request, func(r *managed.ExternalReviewResult) {
		r.Findings = []managed.ExternalFinding{{
			Kind:        "ask-user",
			Description: "is this intended?",
			Code:        "Q001",
			Class:       "design",
			Paths:       []string{"hello.go"},
		}}
	})

	co, err := verify.Complete(context.Background(), verify.CompleteParams{
		WorkDir:          dir,
		RequestPath:      requestPath,
		ReviewResultPath: resultPath,
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if co.Receipt.Outcome != managed.OutcomeNeedsDecision {
		t.Fatalf("Outcome = %q, want needs_decision", co.Receipt.Outcome)
	}
}

func TestComplete_AutoFixableFindingIsFailedRetryable(t *testing.T) {
	dir, _, _ := newTestRepo(t, ".made.yaml", testConfigReviewRequired)
	out, requestPath := prepareRequest(t, dir)
	resultPath := writeExternalResult(t, out.Request, func(r *managed.ExternalReviewResult) {
		r.Findings = []managed.ExternalFinding{{
			Kind:        "auto-fixable",
			Description: "missing gofmt",
			Code:        "FMT001",
			Class:       "style",
			Paths:       []string{"hello.go"},
		}}
	})

	co, err := verify.Complete(context.Background(), verify.CompleteParams{
		WorkDir:          dir,
		RequestPath:      requestPath,
		ReviewResultPath: resultPath,
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if co.Receipt.Outcome != managed.OutcomeFailedRetryable {
		t.Fatalf("Outcome = %q, want failed_retryable", co.Receipt.Outcome)
	}
}

func TestComplete_GuidesAndTaskFlowThroughToRequest(t *testing.T) {
	cfg := "version: 1\nreview:\n  required: true\n  guides:\n    - GUIDE.md\ncommands:\n  test: \"true\"\n"
	dir, _, _ := newTestRepo(t, ".made.yaml", cfg)
	writeTestFile(t, filepath.Join(dir, "GUIDE.md"), "be careful\n")
	gitAt(t, dir, "add", ".")
	gitAt(t, dir, "commit", "-m", "add guide")

	taskPath := filepath.Join(t.TempDir(), "task.md")
	if err := os.WriteFile(taskPath, []byte("implement X"), 0o600); err != nil {
		t.Fatal(err)
	}

	requestPath := filepath.Join(t.TempDir(), "request.json")
	out, err := verify.Prepare(context.Background(), verify.PrepareParams{
		WorkDir:  dir,
		BaseRef:  "origin/main",
		Executor: "cursor",
		Output:   requestPath,
		TaskFile: taskPath,
	})
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if len(out.Request.Contract.Guides) != 1 || out.Request.Contract.Guides[0].Path != "GUIDE.md" {
		t.Fatalf("Contract.Guides = %+v, want GUIDE.md bound", out.Request.Contract.Guides)
	}
	if out.Request.Task == nil || out.Request.Task.Content != "implement X" {
		t.Fatalf("Task = %+v, want embedded task content", out.Request.Task)
	}
}
