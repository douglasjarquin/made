package review

import (
	"context"
	"fmt"
	"os"
	"sort"
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
	filterArgs, err := repositoryFilterOverrides(ctx, worktreePath)
	if err != nil {
		return nil, err
	}
	commandArgs := []string{
		"-C", worktreePath,
		"-c", "core.hooksPath=/dev/null",
		"-c", "core.fsmonitor=false",
		"-c", "diff.external=",
	}
	commandArgs = append(commandArgs, filterArgs...)
	commandArgs = append(commandArgs, args...)
	result, err := exec.Run(ctx, exec.Command{
		Name:        "git",
		Args:        commandArgs,
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

func repositoryFilterOverrides(ctx context.Context, worktreePath string) ([]string, error) {
	result, err := exec.Run(ctx, exec.Command{
		Name: "git",
		Args: []string{
			"-C", worktreePath,
			"config", "--local", "--name-only", "--get-regexp",
			"^filter\\..+\\.(clean|process|smudge)$",
		},
		Env:         controlledGitEnvironment(),
		Timeout:     reviewGitTimeout,
		OutputLimit: reviewGitLimit,
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
		return nil, fmt.Errorf("inspect repository Git filters: output exceeded %d bytes", reviewGitLimit)
	}
	drivers := make(map[string]struct{})
	for key := range strings.FieldsSeq(string(result.Stdout)) {
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
