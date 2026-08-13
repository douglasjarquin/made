package gitgate_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/douglasjarquin/made/internal/gitgate"
)

func TestAddWorktreeChecksOutRef(t *testing.T) {
	dir := t.TempDir()
	barePath := filepath.Join(dir, "gate.git")
	if err := gitgate.InitBare(barePath); err != nil {
		t.Fatalf("InitBare: %v", err)
	}
	if err := gitgate.InstallHooks(barePath, "/usr/bin/true", filepath.Join(dir, "made-home")); err != nil {
		t.Fatalf("InstallHooks: %v", err)
	}
	srcPath := filepath.Join(dir, "src")
	initSourceRepo(t, srcPath)
	if _, err := pushRef(srcPath, barePath); err != nil {
		t.Fatalf("push fixture branch: %v", err)
	}

	worktreesDir := filepath.Join(dir, "worktrees")
	wt, err := gitgate.AddWorktree(barePath, worktreesDir, "refs/heads/main")
	if err != nil {
		t.Fatalf("AddWorktree: %v", err)
	}
	defer func() {
		if err := wt.Remove(); err != nil {
			t.Errorf("Remove: %v", err)
		}
	}()

	if _, err := os.Stat(filepath.Join(wt.Path, "README.md")); err != nil {
		t.Fatalf("expected checked-out fixture file in worktree: %v", err)
	}
}

func TestWorktreeRemovedAfterSimulatedStageFailure(t *testing.T) {
	dir := t.TempDir()
	barePath := filepath.Join(dir, "gate.git")
	if err := gitgate.InitBare(barePath); err != nil {
		t.Fatalf("InitBare: %v", err)
	}
	if err := gitgate.InstallHooks(barePath, "/usr/bin/true", filepath.Join(dir, "made-home")); err != nil {
		t.Fatalf("InstallHooks: %v", err)
	}
	srcPath := filepath.Join(dir, "src")
	initSourceRepo(t, srcPath)
	if _, err := pushRef(srcPath, barePath); err != nil {
		t.Fatalf("push fixture branch: %v", err)
	}

	worktreesDir := filepath.Join(dir, "worktrees")

	runStage := func() (stageErr error) {
		wt, err := gitgate.AddWorktree(barePath, worktreesDir, "refs/heads/main")
		if err != nil {
			return err
		}
		defer func() {
			if rmErr := wt.Remove(); rmErr != nil && stageErr == nil {
				stageErr = rmErr
			}
		}()
		return errors.New("simulated stage failure")
	}

	if err := runStage(); err == nil {
		t.Fatal("expected simulated stage failure to be returned")
	}

	entries, err := os.ReadDir(worktreesDir)
	if err != nil {
		t.Fatalf("read worktrees dir: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected zero leftover worktree dirs, found %d: %v", len(entries), entries)
	}
}
