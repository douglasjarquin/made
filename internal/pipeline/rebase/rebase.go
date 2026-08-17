// Package rebase is stage 2 of made's pipeline (Intent -> Rebase -> Review -> ...):
// it rebases the pushed branch onto the current default branch inside a gate
// worktree, following the same Result.OK/Message chain contract as
// intent.Check so a future orchestrator can treat every stage uniformly.
// Conflicts are always a hard stop - no automatic resolution.
package rebase

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	madeexec "github.com/douglasjarquin/made/internal/exec"
)

const (
	rebaseGitTimeout   = 30 * time.Second
	rebaseGitOutputCap = 1 << 20
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
	return RunContext(context.Background(), worktreePath, defaultBranch)
}

func RunContext(ctx context.Context, worktreePath, defaultBranch string) (Result, error) {
	result, err := runGit(ctx, worktreePath, "rebase", defaultBranch)
	if err != nil {
		return Result{}, fmt.Errorf("rebase: git rebase %s: %w", defaultBranch, err)
	}
	out := append(append([]byte(nil), result.Stdout...), result.Stderr...)
	if result.ExitCode == 0 {
		return Result{
			OK:      true,
			Message: fmt.Sprintf("rebased cleanly onto %s", defaultBranch),
		}, nil
	}

	if !rebaseInProgress(ctx, worktreePath) {
		return Result{}, fmt.Errorf("rebase: git rebase %s failed without unmerged paths: %s", defaultBranch, strings.TrimSpace(string(out)))
	}

	files, err := conflictingFiles(ctx, worktreePath)
	if err != nil {
		return Result{}, fmt.Errorf("rebase: list conflicting files after failed rebase onto %s: %w", defaultBranch, err)
	}
	if len(files) == 0 {
		if err := abortRebase(ctx, worktreePath); err != nil {
			return Result{}, fmt.Errorf("rebase: failed without unmerged paths and abort failed: %w", err)
		}
		return Result{}, fmt.Errorf("rebase: git rebase %s failed without unmerged paths: %s", defaultBranch, strings.TrimSpace(string(out)))
	}

	// A halted stage must never leave the worktree mid-rebase, so whatever
	// runs next (a retry, another stage) always starts from a clean state.
	if err := abortRebase(ctx, worktreePath); err != nil {
		return Result{}, fmt.Errorf("rebase: abort after conflict onto %s: %w", defaultBranch, err)
	}

	return Result{
		OK:               false,
		Message:          fmt.Sprintf("rebase onto %s halted due to conflicts in: %s", defaultBranch, strings.Join(files, ", ")),
		ConflictingFiles: files,
	}, nil
}

func conflictingFiles(ctx context.Context, worktreePath string) ([]string, error) {
	result, err := runGit(ctx, worktreePath, "diff", "--name-only", "--diff-filter=U")
	if err != nil {
		return nil, err
	}
	if result.ExitCode != 0 {
		return nil, fmt.Errorf("git diff --name-only --diff-filter=U exited %d: %s", result.ExitCode, strings.TrimSpace(string(result.Stderr)))
	}

	var files []string
	for _, line := range strings.Split(strings.TrimSpace(string(result.Stdout)), "\n") {
		if line != "" {
			files = append(files, line)
		}
	}
	return files, nil
}

func abortRebase(ctx context.Context, worktreePath string) error {
	result, err := runGit(ctx, worktreePath, "rebase", "--abort")
	if err != nil {
		return fmt.Errorf("git rebase --abort: %w", err)
	}
	if result.ExitCode != 0 {
		return fmt.Errorf("git rebase --abort exited %d: %s", result.ExitCode, strings.TrimSpace(string(result.Stderr)))
	}
	return nil
}

func rebaseInProgress(ctx context.Context, worktreePath string) bool {
	result, err := runGit(ctx, worktreePath, "rev-parse", "--git-dir")
	if err != nil || result.ExitCode != 0 {
		return false
	}

	gitDir := strings.TrimSpace(string(result.Stdout))
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

func runGit(ctx context.Context, worktreePath string, args ...string) (*madeexec.Result, error) {
	commandArgs, err := rebaseGitArgs(ctx, worktreePath, args...)
	if err != nil {
		return nil, err
	}
	return madeexec.Run(ctx, madeexec.Command{
		Name:        "git",
		Args:        commandArgs,
		Env:         controlledRebaseGitEnvironment(),
		Timeout:     rebaseGitTimeout,
		OutputLimit: rebaseGitOutputCap,
	})
}

func rebaseGitArgs(ctx context.Context, worktreePath string, args ...string) ([]string, error) {
	filterArgs, err := repositoryFilterOverrides(ctx, worktreePath)
	if err != nil {
		return nil, err
	}
	commandArgs := []string{
		"-C", worktreePath,
		"-c", "user.name=made-rebase",
		"-c", "user.email=made-rebase@local",
		"-c", "commit.gpgsign=false",
		"-c", "core.hooksPath=/dev/null",
		"-c", "core.fsmonitor=false",
		"-c", "diff.external=",
	}
	commandArgs = append(commandArgs, filterArgs...)
	return append(commandArgs, args...), nil
}

func repositoryFilterOverrides(ctx context.Context, worktreePath string) ([]string, error) {
	result, err := madeexec.Run(ctx, madeexec.Command{
		Name: "git",
		Args: []string{
			"-C", worktreePath,
			"config", "--local", "--name-only", "--get-regexp",
			"^filter\\..+\\.(clean|process|smudge)$",
		},
		Env:         controlledRebaseGitEnvironment(),
		Timeout:     rebaseGitTimeout,
		OutputLimit: rebaseGitOutputCap,
	})
	if err != nil {
		return nil, err
	}
	if result.ExitCode == 1 {
		return nil, nil
	}
	if result.ExitCode != 0 {
		return nil, fmt.Errorf("inspect repository Git filters: git exited %d: %s", result.ExitCode, strings.TrimSpace(string(result.Stderr)))
	}
	if strings.Contains(string(result.Stdout), "[output truncated]") {
		return nil, fmt.Errorf("inspect repository Git filters: output exceeded %d bytes", rebaseGitOutputCap)
	}
	drivers := make(map[string]struct{})
	for _, key := range strings.Fields(string(result.Stdout)) {
		prefix := strings.TrimPrefix(key, "filter.")
		dot := strings.LastIndexByte(prefix, '.')
		if dot <= 0 {
			return nil, fmt.Errorf("inspect repository Git filters: invalid key %q", key)
		}
		switch prefix[dot+1:] {
		case "clean", "process", "smudge":
			drivers[prefix[:dot]] = struct{}{}
		default:
			return nil, fmt.Errorf("inspect repository Git filters: invalid key %q", key)
		}
	}
	names := make([]string, 0, len(drivers))
	for name := range drivers {
		names = append(names, name)
	}
	sort.Strings(names)
	overrides := make([]string, 0, len(names)*8)
	for _, name := range names {
		prefix := "filter." + name + "."
		overrides = append(overrides,
			"-c", prefix+"clean=/bin/cat",
			"-c", prefix+"smudge=/bin/cat",
			"-c", prefix+"process=",
			"-c", prefix+"required=false",
		)
	}
	return overrides, nil
}

func controlledRebaseGitEnvironment() []string {
	env := make([]string, 0, len(os.Environ())+4)
	for _, entry := range os.Environ() {
		name, _, ok := strings.Cut(entry, "=")
		if !ok || strings.HasPrefix(name, "GIT_") || name == "SSH_AUTH_SOCK" {
			continue
		}
		env = append(env, entry)
	}
	return append(env,
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_SYSTEM=/dev/null",
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_TERMINAL_PROMPT=0",
	)
}
