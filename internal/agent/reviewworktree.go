package agent

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/douglasjarquin/made/internal/evidence"
	"github.com/douglasjarquin/made/internal/exec"
)

const (
	reviewPreparationTimeout = 2 * time.Minute
	reviewPreparationLimit   = 1 << 20
)

func prepareReviewWorktree(ctx context.Context, source string) (string, []string, func(), error) {
	source, err := filepath.Abs(source)
	if err != nil {
		return "", nil, nil, fmt.Errorf("resolve source worktree: %w", err)
	}
	headResult, err := runReviewGit(ctx, source, "rev-parse", "--verify", "HEAD^{commit}")
	if err != nil {
		return "", nil, nil, fmt.Errorf("read source HEAD: %w", err)
	}
	if headResult.ExitCode != 0 {
		return "", nil, nil, commandFailure("read source HEAD", headResult)
	}
	head := strings.TrimSpace(string(headResult.Stdout))
	if head == "" {
		return "", nil, nil, fmt.Errorf("read source HEAD returned an empty SHA")
	}
	commonResult, err := runReviewGit(ctx, source, "rev-parse", "--git-common-dir")
	if err != nil {
		return "", nil, nil, fmt.Errorf("read source Git common directory: %w", err)
	}
	if commonResult.ExitCode != 0 {
		return "", nil, nil, commandFailure("read source Git common directory", commonResult)
	}
	protectedPaths, err := reviewProtectedPaths(source, strings.TrimSpace(string(commonResult.Stdout)))
	if err != nil {
		return "", nil, nil, fmt.Errorf("resolve review protected paths: %w", err)
	}

	tempRoot, err := os.MkdirTemp("", "made-review-worktree-")
	if err != nil {
		return "", nil, nil, fmt.Errorf("create review worktree directory: %w", err)
	}
	reviewPath := filepath.Join(tempRoot, "repo")
	cleanupTemp := func() { _ = os.RemoveAll(tempRoot) }

	cloneResult, err := runReviewGit(ctx, "", "clone", "--no-local", "--no-hardlinks", "--no-checkout", source, reviewPath)
	if err != nil {
		cleanupTemp()
		return "", nil, nil, fmt.Errorf("clone review worktree: %w", err)
	}
	if cloneResult.ExitCode != 0 {
		cleanupTemp()
		return "", nil, nil, commandFailure("clone review worktree", cloneResult)
	}
	checkoutResult, err := runReviewGit(ctx, reviewPath, "checkout", "--detach", "--quiet", head)
	if err != nil {
		cleanupTemp()
		return "", nil, nil, fmt.Errorf("checkout review HEAD: %w", err)
	}
	if checkoutResult.ExitCode != 0 {
		cleanupTemp()
		return "", nil, nil, commandFailure("checkout review HEAD", checkoutResult)
	}
	clonedHead, err := runReviewGit(ctx, reviewPath, "rev-parse", "--verify", "HEAD^{commit}")
	if err != nil {
		cleanupTemp()
		return "", nil, nil, fmt.Errorf("verify review HEAD: %w", err)
	}
	if clonedHead.ExitCode != 0 || strings.TrimSpace(string(clonedHead.Stdout)) != head {
		cleanupTemp()
		return "", nil, nil, fmt.Errorf("review clone HEAD %q does not match source HEAD %q", strings.TrimSpace(string(clonedHead.Stdout)), head)
	}
	if err := rejectEscapingSymlinks(reviewPath); err != nil {
		cleanupTemp()
		return "", nil, nil, fmt.Errorf("validate review worktree links: %w", err)
	}
	restoreModes, err := makeReviewTreeReadOnly(reviewPath)
	if err != nil {
		cleanupTemp()
		return "", nil, nil, fmt.Errorf("make review worktree read-only: %w", err)
	}
	cleanup := func() {
		restoreModes()
		cleanupTemp()
	}
	return reviewPath, protectedPaths, cleanup, nil
}

func reviewProtectedPaths(source, commonDir string) ([]string, error) {
	paths := make([]string, 0, 2)
	for _, path := range []string{source, commonDir} {
		if path == "" {
			return nil, fmt.Errorf("Git common directory is empty")
		}
		if !filepath.IsAbs(path) {
			path = filepath.Join(source, path)
		}
		resolved, err := filepath.EvalSymlinks(path)
		if err != nil {
			return nil, err
		}
		paths = append(paths, resolved)
	}
	return paths, nil
}

func runReviewGit(ctx context.Context, dir string, args ...string) (*exec.Result, error) {
	if dir != "" {
		args = append([]string{"-C", dir}, args...)
	}
	return exec.Run(ctx, exec.Command{
		Name:        "git",
		Args:        args,
		Env:         reviewEnvironmentForDir(nil, dir),
		Timeout:     reviewPreparationTimeout,
		OutputLimit: reviewPreparationLimit,
	})
}

func commandFailure(label string, result *exec.Result) error {
	return fmt.Errorf("%s exited %d: stdout=%s stderr=%s", label, result.ExitCode, evidence.RedactString(string(result.Stdout)), evidence.RedactString(string(result.Stderr)))
}

func rejectEscapingSymlinks(root string) error {
	root, err := filepath.EvalSymlinks(root)
	if err != nil {
		return err
	}
	return filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink == 0 {
			return nil
		}
		target, err := filepath.EvalSymlinks(path)
		if err != nil {
			return fmt.Errorf("resolve symlink %q: %w", path, err)
		}
		rel, err := filepath.Rel(root, target)
		if err != nil {
			return fmt.Errorf("relativize symlink %q: %w", path, err)
		}
		if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return fmt.Errorf("symlink %q escapes review worktree", path)
		}
		return nil
	})
}

func makeReviewTreeReadOnly(root string) (func(), error) {
	originalModes := make(map[string]os.FileMode)
	restore := func() {
		for path, mode := range originalModes {
			_ = os.Chmod(path, mode.Perm())
		}
	}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		originalModes[path] = info.Mode()
		mode := info.Mode().Perm() &^ 0o222
		if entry.IsDir() {
			mode = 0o555
		}
		if err := os.Chmod(path, mode); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		restore()
		return nil, err
	}
	return restore, nil
}
