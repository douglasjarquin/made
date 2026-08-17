package review

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/douglasjarquin/made/internal/exec"
)

const (
	reviewGitTimeout = 30 * time.Second
	reviewGitLimit   = 1 << 20
)

func gitOutput(ctx context.Context, worktreePath string, args ...string) (string, error) {
	result, err := runGit(ctx, worktreePath, args, nil)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(result.Stdout)), nil
}

func runGit(ctx context.Context, worktreePath string, args []string, stdin []byte) (*exec.Result, error) {
	args = append([]string{"-C", worktreePath}, args...)
	result, err := exec.Run(ctx, exec.Command{
		Name:        "git",
		Args:        args,
		Env:         controlledGitEnvironment(),
		Stdin:       stdin,
		Timeout:     reviewGitTimeout,
		OutputLimit: reviewGitLimit,
	})
	if err != nil {
		return nil, err
	}
	if result.ExitCode != 0 {
		return result, fmt.Errorf("git exited %d: %s", result.ExitCode, strings.TrimSpace(string(result.Stderr)))
	}
	return result, nil
}

func controlledGitEnvironment() []string {
	env := make([]string, 0, len(os.Environ())+3)
	for _, entry := range os.Environ() {
		name, _, ok := strings.Cut(entry, "=")
		if !ok || strings.HasPrefix(name, "GIT_") || name == "SSH_AUTH_SOCK" {
			continue
		}
		env = append(env, entry)
	}
	env = append(env, "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null", "GIT_TERMINAL_PROMPT=0")
	return env
}
