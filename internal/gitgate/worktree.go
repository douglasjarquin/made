package gitgate

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// bareRepoEnv returns environment variables for running git commands on bare repositories.
// It sets safe.bareRepository=all to allow operations on bare repos when the system
// has safe.bareRepository=explicit configured.
func bareRepoEnv() []string {
	return []string{
		"GIT_CONFIG_COUNT=1",
		"GIT_CONFIG_KEY_0=safe.bareRepository",
		"GIT_CONFIG_VALUE_0=all",
	}
}

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
	cmd.Env = append(os.Environ(), bareRepoEnv()...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("gitgate: git worktree add %s %s: %w: %s", slot, ref, err, strings.TrimSpace(string(out)))
	}
	return &Worktree{Path: slot, barePath: barePath}, nil
}

func (w *Worktree) Remove() error {
	cmd := exec.Command("git", "worktree", "remove", "--force", w.Path)
	cmd.Dir = w.barePath
	cmd.Env = append(os.Environ(), bareRepoEnv()...)
	out, err := cmd.CombinedOutput()
	if err == nil {
		return nil
	}

	if rmErr := os.RemoveAll(w.Path); rmErr != nil {
		return fmt.Errorf("gitgate: git worktree remove %s: %v: %s; fallback rm -rf failed: %w", w.Path, err, strings.TrimSpace(string(out)), rmErr)
	}
	pruneCmd := exec.Command("git", "worktree", "prune")
	pruneCmd.Dir = w.barePath
	pruneCmd.Env = append(os.Environ(), bareRepoEnv()...)
	_ = pruneCmd.Run()
	return nil
}
