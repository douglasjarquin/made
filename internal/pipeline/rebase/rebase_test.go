package rebase_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/douglasjarquin/made/internal/pipeline/rebase"
)

func TestRun_CleanRebaseProceeds(t *testing.T) {
	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	t.Setenv("GIT_CONFIG_SYSTEM", "/dev/null")
	t.Setenv("SSH_AUTH_SOCK", "")
	f := setupFixture(t, "", "", "")
	wt := f.addWorktree(t)
	defer func() {
		if err := wt.Remove(); err != nil {
			t.Errorf("Remove: %v", err)
		}
	}()

	result, err := rebase.Run(wt.Path, f.defaultBranch)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !result.OK {
		t.Fatalf("expected OK=true for clean rebase, got %+v", result)
	}
	if len(result.ConflictingFiles) != 0 {
		t.Fatalf("expected no conflicting files, got %v", result.ConflictingFiles)
	}

	if _, statErr := os.Stat(filepath.Join(wt.Path, "main-only.txt")); statErr != nil {
		t.Fatalf("expected worktree HEAD to include main's commit (main-only.txt missing): %v", statErr)
	}
	if _, statErr := os.Stat(filepath.Join(wt.Path, "feature-only.txt")); statErr != nil {
		t.Fatalf("expected worktree HEAD to still include feature's commit (feature-only.txt missing): %v", statErr)
	}

	log := run(t, wt.Path, "log", "--format=%s")
	if !strings.Contains(log, "main commit") || !strings.Contains(log, "feature commit") {
		t.Fatalf("expected rebased history to contain both commits, got log:\n%s", log)
	}
}

func TestRun_CleanRebaseIgnoresAmbientGitRouting(t *testing.T) {
	f := setupFixture(t, "", "", "")
	wt := f.addWorktree(t)
	defer func() {
		if err := wt.Remove(); err != nil {
			t.Errorf("Remove: %v", err)
		}
	}()

	ambientGitDir := t.TempDir()
	run(t, ambientGitDir, "init", "--bare", "-q")
	t.Setenv("GIT_DIR", ambientGitDir)

	result, err := rebase.Run(wt.Path, f.defaultBranch)
	if err != nil {
		t.Fatalf("Run with ambient GIT_DIR: %v", err)
	}
	if !result.OK {
		t.Fatalf("expected OK=true despite ambient GIT_DIR, got %+v", result)
	}
}

func TestRun_ConflictingRebaseHalts(t *testing.T) {
	f := setupFixture(t, "shared.txt", "main version\n", "feature version\n")
	wt := f.addWorktree(t)
	defer func() {
		if err := wt.Remove(); err != nil {
			t.Errorf("Remove: %v", err)
		}
	}()

	result, err := rebase.Run(wt.Path, f.defaultBranch)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.OK {
		t.Fatalf("expected OK=false for conflicting rebase, got %+v", result)
	}
	if len(result.ConflictingFiles) != 1 || result.ConflictingFiles[0] != "shared.txt" {
		t.Fatalf("expected ConflictingFiles=[shared.txt], got %v", result.ConflictingFiles)
	}
	if !strings.Contains(result.Message, "shared.txt") {
		t.Fatalf("expected message to name the conflicting file, got %q", result.Message)
	}

	status := run(t, wt.Path, "status", "--porcelain=v2", "--branch")
	if strings.Contains(strings.ToLower(status), "rebase") {
		t.Fatalf("expected worktree to be left clean (no rebase in progress), got status:\n%s", status)
	}

	gitDirOut := run(t, wt.Path, "rev-parse", "--git-dir")
	gitDir := strings.TrimSpace(gitDirOut)
	if !filepath.IsAbs(gitDir) {
		gitDir = filepath.Join(wt.Path, gitDir)
	}
	for _, marker := range []string{"rebase-merge", "rebase-apply"} {
		if _, statErr := os.Stat(filepath.Join(gitDir, marker)); statErr == nil {
			t.Fatalf("expected no %s marker after abort", marker)
		}
	}
}
