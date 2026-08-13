package test_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/douglasjarquin/made/internal/evidence"
	pipelinetest "github.com/douglasjarquin/made/internal/pipeline/test"
)

func TestRun_FailingCommandBlocksAndCapturesEvidence(t *testing.T) {
	f := setupFixture(t)
	wt := f.addWorktree(t)
	defer func() {
		if err := wt.Remove(); err != nil {
			t.Errorf("Remove: %v", err)
		}
	}()

	evidenceRoot := t.TempDir()
	store := &evidence.InRepoStore{RepoPath: evidenceRoot, Dir: ".made/evidence"}
	runID := "run-failing"

	testCommand := []string{"sh", "-c", "printf 'stage stdout line\\n'; printf 'stage stderr line\\n' 1>&2; exit 7"}

	result, err := pipelinetest.Run(context.Background(), wt.Path, runID, testCommand, store)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if result.OK {
		t.Fatalf("expected OK=false for a non-zero exit, got %+v", result)
	}
	if result.ExitCode != 7 {
		t.Fatalf("expected ExitCode=7, got %d", result.ExitCode)
	}
	if result.Message == "" {
		t.Fatal("expected a non-empty Message describing the failure")
	}

	stdout, err := os.ReadFile(filepath.Join(evidenceRoot, ".made/evidence", runID, "stdout.log"))
	if err != nil {
		t.Fatalf("read stdout evidence: %v", err)
	}
	if !strings.Contains(string(stdout), "stage stdout line") {
		t.Fatalf("expected stdout evidence to contain full command stdout, got %q", stdout)
	}

	stderr, err := os.ReadFile(filepath.Join(evidenceRoot, ".made/evidence", runID, "stderr.log"))
	if err != nil {
		t.Fatalf("read stderr evidence: %v", err)
	}
	if !strings.Contains(string(stderr), "stage stderr line") {
		t.Fatalf("expected stderr evidence to contain full command stderr, got %q", stderr)
	}
}

func TestRun_PassingCommandProceedsAndCapturesEvidence(t *testing.T) {
	f := setupFixture(t)
	wt := f.addWorktree(t)
	defer func() {
		if err := wt.Remove(); err != nil {
			t.Errorf("Remove: %v", err)
		}
	}()

	evidenceRoot := t.TempDir()
	store := &evidence.InRepoStore{RepoPath: evidenceRoot, Dir: ".made/evidence"}
	runID := "run-passing"

	testCommand := []string{"sh", "-c", "printf 'all tests passed\\n'; exit 0"}

	result, err := pipelinetest.Run(context.Background(), wt.Path, runID, testCommand, store)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if !result.OK {
		t.Fatalf("expected OK=true for a zero exit, got %+v", result)
	}
	if result.ExitCode != 0 {
		t.Fatalf("expected ExitCode=0, got %d", result.ExitCode)
	}

	stdout, err := os.ReadFile(filepath.Join(evidenceRoot, ".made/evidence", runID, "stdout.log"))
	if err != nil {
		t.Fatalf("read stdout evidence: %v", err)
	}
	if !strings.Contains(string(stdout), "all tests passed") {
		t.Fatalf("expected stdout evidence to contain full command stdout, got %q", stdout)
	}
}

func TestRun_RespectsConfiguredCommandExactly(t *testing.T) {
	f := setupFixture(t)
	wt := f.addWorktree(t)
	defer func() {
		if err := wt.Remove(); err != nil {
			t.Errorf("Remove: %v", err)
		}
	}()

	writeFile(t, wt.Path, "focused-test-marker.txt", "focused subset only\n")

	store := &evidence.InRepoStore{RepoPath: t.TempDir(), Dir: ".made/evidence"}
	testCommand := []string{"sh", "-c", "test -f focused-test-marker.txt"}

	result, err := pipelinetest.Run(context.Background(), wt.Path, "run-focused", testCommand, store)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !result.OK {
		t.Fatalf("expected the exact configured focused command to run inside worktreePath and pass, got %+v", result)
	}
}
