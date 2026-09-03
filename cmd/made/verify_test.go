package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/douglasjarquin/made/internal/managed"
)

func gitVerifyAt(t *testing.T, dir string, args ...string) string {
	t.Helper()
	full := append([]string{"-C", dir, "-c", "commit.gpgsign=false"}, args...)
	out, err := exec.Command("git", full...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func newVerifyTestRepo(t *testing.T) string {
	t.Helper()
	dir := shortTempDir(t)

	gitVerifyAt(t, dir, "init", "-b", "main")
	gitVerifyAt(t, dir, "config", "user.email", "test@test.local")
	gitVerifyAt(t, dir, "config", "user.name", "test")

	if err := os.WriteFile(filepath.Join(dir, ".made.yaml"), []byte("version: 1\ncommands:\n  test: \"true\"\n  lint: \"true\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitVerifyAt(t, dir, "add", ".")
	gitVerifyAt(t, dir, "commit", "-m", "initial")
	baseSHA := gitVerifyAt(t, dir, "rev-parse", "HEAD")
	gitVerifyAt(t, dir, "update-ref", "refs/remotes/origin/main", baseSHA)

	if err := os.WriteFile(filepath.Join(dir, "hello.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitVerifyAt(t, dir, "add", ".")
	gitVerifyAt(t, dir, "commit", "-m", "add hello.go")

	return dir
}

func TestVerifyRun_JSONHappyPath(t *testing.T) {
	dir := newVerifyTestRepo(t)

	stdout, stderr, code := runCapture(t, []string{"verify", "run", "--repo", dir, "--base-ref", "origin/main", "--json"})
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr=%s", code, stderr)
	}
	var receipt struct {
		Outcome string `json:"outcome"`
		Stages  []struct {
			Name   string `json:"name"`
			Status string `json:"status"`
		} `json:"stages"`
	}
	if err := json.Unmarshal(stdout, &receipt); err != nil {
		t.Fatalf("parse JSON output: %v (stdout=%s)", err, stdout)
	}
	if receipt.Outcome != string(managed.OutcomePassed) {
		t.Errorf("outcome = %q, want passed", receipt.Outcome)
	}
}

func TestVerifyRun_MissingBaseRefIsUsageFriendlyFailure(t *testing.T) {
	dir := newVerifyTestRepo(t)

	_, stderr, code := runCapture(t, []string{"verify", "run", "--repo", dir})
	if code == 0 {
		t.Fatalf("expected a non-zero exit when --base-ref is missing")
	}
	if len(stderr) == 0 {
		t.Error("expected a diagnostic on stderr")
	}
}

func TestVerifyRun_RejectsExternalReviewSource(t *testing.T) {
	dir := newVerifyTestRepo(t)

	_, _, code := runCapture(t, []string{"verify", "run", "--repo", dir, "--base-ref", "origin/main", "--review-source", "external"})
	if code != 2 {
		t.Fatalf("exit code = %d, want 2 (usage error)", code)
	}
}

func TestVerifyPrepareComplete_CLIRoundTrip(t *testing.T) {
	dir := newVerifyTestRepo(t)
	requestPath := filepath.Join(shortTempDir(t), "request.json")

	stdout, stderr, code := runCapture(t, []string{
		"verify", "prepare",
		"--repo", dir,
		"--base-ref", "origin/main",
		"--executor", "cursor",
		"--output", requestPath,
		"--json",
	})
	if code != 0 {
		t.Fatalf("prepare exit code = %d, want 0; stderr=%s", code, stderr)
	}
	var prep prepareReport
	if err := json.Unmarshal(stdout, &prep); err != nil {
		t.Fatalf("parse prepare JSON: %v (stdout=%s)", err, stdout)
	}
	if prep.RequestPath != requestPath {
		t.Errorf("RequestPath = %q, want %q", prep.RequestPath, requestPath)
	}

	resultPath := filepath.Join(shortTempDir(t), "result.json")
	result := managed.ExternalReviewResult{
		SchemaVersion:         managed.ExternalReviewSchemaVersion,
		ReviewContractVersion: managed.ReviewContractVersion,
		Executor:              "cursor-agent",
		Reviewer:              "claude",
		BaseSHA:               prep.BaseSHA,
		InputSHA:              prep.InputSHA,
		PolicyHash:            prep.ConfigHash,
		ReviewContractHash:    prep.ContractHash,
		Findings:              []managed.ExternalFinding{},
	}
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(resultPath, data, 0o600); err != nil {
		t.Fatal(err)
	}

	stdout, stderr, code = runCapture(t, []string{
		"verify", "complete",
		"--repo", dir,
		"--request", requestPath,
		"--review-result", resultPath,
		"--json",
	})
	if code != 0 {
		t.Fatalf("complete exit code = %d, want 0; stderr=%s", code, stderr)
	}
	var receipt struct {
		Outcome string `json:"outcome"`
	}
	if err := json.Unmarshal(stdout, &receipt); err != nil {
		t.Fatalf("parse complete JSON: %v (stdout=%s)", err, stdout)
	}
	if receipt.Outcome != string(managed.OutcomePassed) {
		t.Fatalf("outcome = %q, want passed", receipt.Outcome)
	}
}

func TestVerifyStatusAndReceipt_CLI(t *testing.T) {
	dir := newVerifyTestRepo(t)
	inputSHA := gitVerifyAt(t, dir, "rev-parse", "HEAD")

	if _, stderr, code := runCapture(t, []string{"verify", "run", "--repo", dir, "--base-ref", "origin/main", "--json"}); code != 0 {
		t.Fatalf("verify run: exit %d stderr=%s", code, stderr)
	}

	stdout, stderr, code := runCapture(t, []string{"verify", "status", "--repo", dir, "--json"})
	if code != 0 {
		t.Fatalf("verify status: exit %d stderr=%s", code, stderr)
	}
	var status statusReport
	if err := json.Unmarshal(stdout, &status); err != nil {
		t.Fatalf("parse status JSON: %v (stdout=%s)", err, stdout)
	}
	if !status.Found || status.Receipt == nil {
		t.Fatalf("status = %+v, want a found receipt", status)
	}

	stdout, stderr, code = runCapture(t, []string{"verify", "receipt", "--repo", dir, "--json", inputSHA})
	if code != 0 {
		t.Fatalf("verify receipt: exit %d stderr=%s", code, stderr)
	}
	var receipt statusReport
	if err := json.Unmarshal(stdout, &receipt); err != nil {
		t.Fatalf("parse receipt JSON: %v (stdout=%s)", err, stdout)
	}
	if !receipt.Found {
		t.Fatalf("expected the receipt to be found by exact input SHA")
	}
}

func TestVerifyClean_CLI(t *testing.T) {
	dir := newVerifyTestRepo(t)
	if _, stderr, code := runCapture(t, []string{"verify", "run", "--repo", dir, "--base-ref", "origin/main", "--json"}); code != 0 {
		t.Fatalf("verify run: exit %d stderr=%s", code, stderr)
	}
	stdout, stderr, code := runCapture(t, []string{"verify", "clean", "--repo", dir})
	if code != 0 {
		t.Fatalf("verify clean: exit %d stderr=%s", code, stderr)
	}
	if len(stdout) == 0 {
		t.Error("expected clean to report the removed directory")
	}
}

func TestVerifyPrepare_MissingExecutorFails(t *testing.T) {
	dir := newVerifyTestRepo(t)
	_, stderr, code := runCapture(t, []string{"verify", "prepare", "--repo", dir, "--base-ref", "origin/main"})
	if code == 0 {
		t.Fatal("expected an error when --executor is missing")
	}
	if len(stderr) == 0 {
		t.Error("expected a diagnostic on stderr")
	}
}
