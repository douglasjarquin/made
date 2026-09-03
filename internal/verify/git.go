package verify

import (
	"context"
	"fmt"

	"github.com/douglasjarquin/made/internal/safegit"
)

func repoRoot(ctx context.Context, dir string) (string, error) {
	out, err := safegit.Output(ctx, safegit.Command{WorktreePath: dir, Args: []string{"rev-parse", "--show-toplevel"}})
	if err != nil {
		return "", fmt.Errorf("verify: resolve repository root: %w", err)
	}
	return out, nil
}

func headCommit(ctx context.Context, root string) (string, error) {
	out, err := safegit.Output(ctx, safegit.Command{WorktreePath: root, Args: []string{"rev-parse", "--verify", "HEAD^{commit}"}})
	if err != nil {
		return "", fmt.Errorf("verify: resolve HEAD (repository must have at least one commit): %w", err)
	}
	return out, nil
}

func worktreeStatus(ctx context.Context, root string) (string, error) {
	out, err := safegit.Output(ctx, safegit.Command{WorktreePath: root, Args: []string{"status", "--porcelain", "--untracked-files=all"}})
	if err != nil {
		return "", fmt.Errorf("verify: inspect worktree status: %w", err)
	}
	return out, nil
}

func resolveCommit(ctx context.Context, root, ref string) (string, error) {
	out, err := safegit.Output(ctx, safegit.Command{WorktreePath: root, Args: []string{"rev-parse", "--verify", ref + "^{commit}"}})
	if err != nil {
		return "", fmt.Errorf("verify: base ref %q was not found locally; made never fetches automatically, so make sure it exists locally first: %w", ref, err)
	}
	return out, nil
}

func mergeBase(ctx context.Context, root, a, b string) (string, error) {
	out, err := safegit.Output(ctx, safegit.Command{WorktreePath: root, Args: []string{"merge-base", a, b}})
	if err != nil {
		return "", fmt.Errorf("verify: no common ancestor between %q and %q: %w", a, b, err)
	}
	return out, nil
}

func remoteOriginURL(ctx context.Context, root string) string {
	out, err := safegit.Output(ctx, safegit.Command{WorktreePath: root, Args: []string{"remote", "get-url", "origin"}})
	if err != nil {
		return ""
	}
	return out
}
