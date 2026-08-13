package lint_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/douglasjarquin/made/internal/evidence"
	pipelinelint "github.com/douglasjarquin/made/internal/pipeline/lint"
)

func TestRun_FailingLintCommandBlocksAndCapturesEvidence(t *testing.T) {
	f := setupFixture(t)
	wt := f.addWorktree(t)
	defer func() {
		if err := wt.Remove(); err != nil {
			t.Errorf("Remove: %v", err)
		}
	}()

	evidenceRoot := t.TempDir()
	store := &evidence.InRepoStore{RepoPath: evidenceRoot, Dir: ".made/evidence"}
	runID := "run-failing-lint"

	lintCommand := []string{"sh", "-c", "printf 'lint stdout line\\n'; printf 'lint stderr line\\n' 1>&2; exit 1"}

	result, err := pipelinelint.Run(context.Background(), wt.Path, runID, lintCommand, store)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if result.OK {
		t.Fatalf("expected OK=false for a non-zero exit, got %+v", result)
	}
	if result.ExitCode != 1 {
		t.Fatalf("expected ExitCode=1, got %d", result.ExitCode)
	}
	if result.Message == "" {
		t.Fatal("expected a non-empty Message describing the failure")
	}

	stdout, err := os.ReadFile(filepath.Join(evidenceRoot, ".made/evidence", runID, "stdout.log"))
	if err != nil {
		t.Fatalf("read stdout evidence: %v", err)
	}
	if !strings.Contains(string(stdout), "lint stdout line") {
		t.Fatalf("expected stdout evidence to contain full command stdout, got %q", stdout)
	}

	stderr, err := os.ReadFile(filepath.Join(evidenceRoot, ".made/evidence", runID, "stderr.log"))
	if err != nil {
		t.Fatalf("read stderr evidence: %v", err)
	}
	if !strings.Contains(string(stderr), "lint stderr line") {
		t.Fatalf("expected stderr evidence to contain full command stderr, got %q", stderr)
	}
}

func TestRun_PassingLintCommandProceedsAndCapturesEvidence(t *testing.T) {
	f := setupFixture(t)
	wt := f.addWorktree(t)
	defer func() {
		if err := wt.Remove(); err != nil {
			t.Errorf("Remove: %v", err)
		}
	}()

	evidenceRoot := t.TempDir()
	store := &evidence.InRepoStore{RepoPath: evidenceRoot, Dir: ".made/evidence"}
	runID := "run-passing-lint"

	lintCommand := []string{"sh", "-c", "printf 'no lint issues\\n'; exit 0"}

	result, err := pipelinelint.Run(context.Background(), wt.Path, runID, lintCommand, store)
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
	if !strings.Contains(string(stdout), "no lint issues") {
		t.Fatalf("expected stdout evidence to contain full command stdout, got %q", stdout)
	}
}

func TestRun_UnsetLintCommandFallsBackGracefully(t *testing.T) {
	f := setupFixture(t)
	wt := f.addWorktree(t)
	defer func() {
		if err := wt.Remove(); err != nil {
			t.Errorf("Remove: %v", err)
		}
	}()

	store := &evidence.InRepoStore{RepoPath: t.TempDir(), Dir: ".made/evidence"}

	result, err := pipelinelint.Run(context.Background(), wt.Path, "run-unset-lint", nil, store)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !result.OK {
		t.Fatalf("expected OK=true when no lint command is configured, got %+v", result)
	}
	if result.Message == "" {
		t.Fatal("expected a non-empty Message explaining the no-op fallback")
	}
}
