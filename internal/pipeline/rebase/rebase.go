// Package rebase is stage 2 of made's pipeline (Intent -> Rebase -> Review -> ...):
// it rebases the pushed branch onto the current default branch inside a gate
// worktree, following the same Result.OK/Message chain contract as
// intent.Check so a future orchestrator can treat every stage uniformly.
// Conflicts are always a hard stop - no automatic resolution.
package rebase

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type Result struct {
	OK               bool
	Message          string
	ConflictingFiles []string
}

// Run's error return is reserved for infrastructure failures (worktreePath
// unreadable, git missing, defaultBranch unresolvable, abort itself failing,
// etc); a rebase conflict is a normal outcome reported via Result.OK, not an
// error.
func Run(worktreePath, defaultBranch string) (Result, error) {
	cmd := exec.Command("git", "-C", worktreePath, "rebase", defaultBranch)
	out, rebaseErr := cmd.CombinedOutput()
	if rebaseErr == nil {
		return Result{
			OK:      true,
			Message: fmt.Sprintf("rebased cleanly onto %s", defaultBranch),
		}, nil
	}

	if !rebaseInProgress(worktreePath) {
		return Result{}, fmt.Errorf("rebase: git rebase %s: %w: %s", defaultBranch, rebaseErr, strings.TrimSpace(string(out)))
	}

	files, err := conflictingFiles(worktreePath)
	if err != nil {
		return Result{}, fmt.Errorf("rebase: list conflicting files after failed rebase onto %s: %w", defaultBranch, err)
	}

	// A halted stage must never leave the worktree mid-rebase, so whatever
	// runs next (a retry, another stage) always starts from a clean state.
	if err := abortRebase(worktreePath); err != nil {
		return Result{}, fmt.Errorf("rebase: abort after conflict onto %s: %w", defaultBranch, err)
	}

	return Result{
		OK:               false,
		Message:          fmt.Sprintf("rebase onto %s halted due to conflicts in: %s", defaultBranch, strings.Join(files, ", ")),
		ConflictingFiles: files,
	}, nil
}

func conflictingFiles(worktreePath string) ([]string, error) {
	cmd := exec.Command("git", "-C", worktreePath, "diff", "--name-only", "--diff-filter=U")
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	var files []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line != "" {
			files = append(files, line)
		}
	}
	return files, nil
}

func abortRebase(worktreePath string) error {
	cmd := exec.Command("git", "-C", worktreePath, "rebase", "--abort")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git rebase --abort: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func rebaseInProgress(worktreePath string) bool {
	out, err := exec.Command("git", "-C", worktreePath, "rev-parse", "--git-dir").Output()
	if err != nil {
		return false
	}

	gitDir := strings.TrimSpace(string(out))
	if !filepath.IsAbs(gitDir) {
		gitDir = filepath.Join(worktreePath, gitDir)
	}

	for _, marker := range []string{"rebase-merge", "rebase-apply"} {
		if _, statErr := os.Stat(filepath.Join(gitDir, marker)); statErr == nil {
			return true
		}
	}
	return false
}
