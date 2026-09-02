package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func runGitForPlanTest(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=plan-cmd-test", "GIT_AUTHOR_EMAIL=plan-cmd-test@example.com",
		"GIT_COMMITTER_NAME=plan-cmd-test", "GIT_COMMITTER_EMAIL=plan-cmd-test@example.com",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v in %s failed: %v: %s", args, dir, err, out)
	}
	return string(out)
}

func initRepoForPlanTest(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	runGitForPlanTest(t, dir, "init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("write README.md: %v", err)
	}
	runGitForPlanTest(t, dir, "add", "-A")
	runGitForPlanTest(t, dir, "commit", "-q", "-m", "base commit")
	return dir
}

func TestPlanCommand_JSONIsSideEffectFree(t *testing.T) {
	dir := initRepoForPlanTest(t)
	before := runGitForPlanTest(t, dir, "status", "--porcelain", "--branch")

	stdout, stderr, code := runCapture(t, []string{"plan", "--json", "--base", "main", "--repo", dir})
	if code != 0 {
		t.Fatalf("expected exit 0, got %d, stderr:\n%s", code, stderr)
	}

	after := runGitForPlanTest(t, dir, "status", "--porcelain", "--branch")
	if before != after {
		t.Fatalf("expected made plan to be side-effect-free, git status changed:\nbefore:\n%s\nafter:\n%s", before, after)
	}

	var report struct {
		PlanVersion int `json:"plan_version"`
		Stages      []struct {
			Name   string `json:"name"`
			Action string `json:"action"`
			Reason string `json:"reason"`
		} `json:"stages"`
	}
	if err := json.Unmarshal(stdout, &report); err != nil {
		t.Fatalf("decode plan JSON: %v\noutput:\n%s", err, stdout)
	}
	if report.PlanVersion != 1 {
		t.Fatalf("expected plan_version 1, got %d", report.PlanVersion)
	}
	found := false
	for _, s := range report.Stages {
		if s.Name == "push" {
			found = true
			if s.Action != "run" || s.Reason == "" {
				t.Fatalf("expected push stage to always run with a reason, got %+v", s)
			}
		}
	}
	if !found {
		t.Fatalf("expected a push stage decision in the plan, got %+v", report.Stages)
	}
}

func TestPlanCommand_HumanOutputListsStagesAndReasons(t *testing.T) {
	dir := initRepoForPlanTest(t)

	stdout, stderr, code := runCapture(t, []string{"plan", "--base", "main", "--repo", dir})
	if code != 0 {
		t.Fatalf("expected exit 0, got %d, stderr:\n%s", code, stderr)
	}
	out := string(stdout)
	for _, want := range []string{"Push", "external side effect", "Candidate:", "Base:"} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected human plan output to contain %q, got:\n%s", want, out)
		}
	}
}
