// Package push is stage 7 of made's pipeline (Intent -> Rebase -> Review ->
// Test -> Document -> Lint -> Push -> ...): it pushes the validated, rebased
// worktree branch to the repo's real remote (the one sitting in front of
// made's own gate bare repo). It never opens a pull request or triggers any
// merge - that is Task 18's PR stage, layered on top of a successful Push.
package push

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/douglasjarquin/made/internal/exec"
)

type Result struct {
	OK      bool
	Message string
}

// credentialInURL matches the userinfo component of a URL (scheme://user:pass@host)
// so it can be stripped from anything made surfaces in a Result or error - git
// itself prints the real remote URL verbatim into its own error output when a
// push fails (e.g. "unable to access 'https://user:token@host/repo.git/'"),
// and that string must never end up in made's own logs, DB, or evidence.
var credentialInURL = regexp.MustCompile(`([a-zA-Z][a-zA-Z0-9+.-]*://)[^\s/@]+@`)

// Run's error return is reserved for infrastructure failures (empty
// arguments, git failing to start); a push rejected by the remote - auth
// failure, unreachable host, non-fast-forward - is a normal outcome reported
// via Result.OK, not an error, following the lint/test stage convention.
//
// Run never reads or constructs the remote's URL itself: it shells out to
// `git push <remoteName> <branch>` inside the worktree, so git resolves the
// destination and any credentials entirely from the worktree's own git
// config and credential helper. made never holds the credential material,
// so there is nothing for made's own state to persist or leak - the
// redaction below only guards against git echoing the remote URL back into
// its own output on failure.
func Run(ctx context.Context, worktreePath, remoteName, branch string) (Result, error) {
	if strings.TrimSpace(worktreePath) == "" {
		return Result{}, fmt.Errorf("push: worktreePath must not be empty")
	}
	if strings.TrimSpace(remoteName) == "" {
		return Result{}, fmt.Errorf("push: remoteName must not be empty")
	}
	if strings.TrimSpace(branch) == "" {
		return Result{}, fmt.Errorf("push: branch must not be empty")
	}

	// A gate worktree checked out from a fully-qualified ref (as
	// gitgate.AddWorktree does) ends up on a detached HEAD rather than a
	// local branch of the same name, so the source side of the refspec must
	// be HEAD, not the branch name.
	refspec := fmt.Sprintf("HEAD:refs/heads/%s", branch)
	res, err := exec.Run(ctx, exec.Command{
		Name: "git",
		Args: []string{"push", remoteName, refspec},
		Dir:  worktreePath,
	})
	if err != nil {
		return Result{}, fmt.Errorf("push: git push %s %s: %s", remoteName, refspec, redact(err.Error()))
	}

	if res.ExitCode != 0 {
		output := redact(strings.TrimSpace(string(res.Stdout) + "\n" + string(res.Stderr)))
		return Result{
			OK:      false,
			Message: fmt.Sprintf("git push %s %s failed with exit code %d: %s", remoteName, refspec, res.ExitCode, output),
		}, nil
	}

	return Result{
		OK:      true,
		Message: fmt.Sprintf("pushed %s to %s", branch, remoteName),
	}, nil
}

func redact(s string) string {
	return credentialInURL.ReplaceAllString(s, "${1}REDACTED@")
}
