package receipt

import (
	"context"
	"strings"

	execpkg "github.com/douglasjarquin/made/internal/exec"
)

// RepoIdentity returns repoPath's configured origin remote URL for use as a
// Fingerprint's RepoIdentity field, or "" on any failure (no origin
// configured, repoPath is not a git repository, git itself is unavailable) -
// it fails open like every other Fingerprint input, since a wrong-but-stable
// identity only ever prevents a reuse hit, never causes a false one.
func RepoIdentity(ctx context.Context, repoPath string) string {
	res, err := execpkg.Run(ctx, execpkg.Command{
		Name: "git",
		Args: []string{"remote", "get-url", "origin"},
		Dir:  repoPath,
	})
	if err != nil || res.ExitCode != 0 {
		return ""
	}
	return strings.TrimSpace(string(res.Stdout))
}
