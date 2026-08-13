package gitgate

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

type Worktree struct {
	Path     string
	barePath string
}

func AddWorktree(barePath, worktreesDir, ref string) (*Worktree, error) {
	if err := os.MkdirAll(worktreesDir, 0o755); err != nil {
		return nil, fmt.Errorf("gitgate: create worktrees dir: %w", err)
	}
	slot, err := os.MkdirTemp(worktreesDir, "run-*")
	if err != nil {
		return nil, fmt.Errorf("gitgate: reserve worktree slot: %w", err)
	}
	if err := os.Remove(slot); err != nil {
		return nil, fmt.Errorf("gitgate: free reserved worktree slot: %w", err)
	}

	cmd := exec.Command("git", "worktree", "add", slot, ref)
	cmd.Dir = barePath
	if out, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("gitgate: git worktree add %s %s: %w: %s", slot, ref, err, strings.TrimSpace(string(out)))
	}
	return &Worktree{Path: slot, barePath: barePath}, nil
}

func (w *Worktree) Remove() error {
	cmd := exec.Command("git", "worktree", "remove", "--force", w.Path)
	cmd.Dir = w.barePath
	out, err := cmd.CombinedOutput()
	if err == nil {
		return nil
	}

	if rmErr := os.RemoveAll(w.Path); rmErr != nil {
		return fmt.Errorf("gitgate: git worktree remove %s: %v: %s; fallback rm -rf failed: %w", w.Path, err, strings.TrimSpace(string(out)), rmErr)
	}
	pruneCmd := exec.Command("git", "worktree", "prune")
	pruneCmd.Dir = w.barePath
	_ = pruneCmd.Run()
	return nil
}
