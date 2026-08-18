package github_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/douglasjarquin/made/internal/github/githubtest"
)

func TestStrictFakeGHRejectsUnsupportedJSONFields(t *testing.T) {
	bin := githubtest.Build(t)
	scenarioDir := t.TempDir()
	cmd := exec.Command(bin, "pr", "view", "https://github.com/example/repo/pull/1", "--json", "mergeStateStatus", "--unexpected")
	cmd.Env = append(os.Environ(), "FAKE_GH_STATE_DIR="+scenarioDir)
	if output, err := cmd.CombinedOutput(); err == nil {
		t.Fatalf("strict fake accepted unsupported invocation, output=%s", output)
	}
}

func TestStrictFakeGHRejectsPRURLAsWorkflowRunID(t *testing.T) {
	bin := githubtest.Build(t)
	logPath := filepath.Join(t.TempDir(), "gh.log")
	cmd := exec.Command(bin, "run", "view", "https://github.com/example/repo/pull/1", "--log")
	cmd.Env = append(os.Environ(), "FAKE_GH_LOG_FILE="+logPath)
	if output, err := cmd.CombinedOutput(); err == nil {
		t.Fatalf("strict fake accepted PR URL as workflow run ID, output=%s", output)
	}
}

func TestStrictFakeGHInvocationLogDoesNotAcceptLegacyMergeStateCommand(t *testing.T) {
	bin := githubtest.Build(t)
	cmd := exec.Command(bin, "pr", "view", "https://github.com/example/repo/pull/1", "--json", "mergeStateStatus")
	if output, err := cmd.CombinedOutput(); err == nil {
		t.Fatalf("legacy merge-state invocation was accepted: %s", output)
	}
}
