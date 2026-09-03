// Package safegit provides a safe Git execution primitive for use in
// managed-validation mode.
//
// All invocations strip hostile environment variables, neutralize hooks and
// credential helpers, and use explicit argv with no shell interpolation.
// No network Git operation is performed.
package safegit

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
	DefaultTimeout     = 30 * time.Second
	DefaultOutputLimit = 1 << 20
)

// Command describes a safe Git invocation.
type Command struct {
	// WorktreePath is the -C argument; must be an absolute path.
	WorktreePath string
	// Args are the Git subcommand and its arguments.
	Args []string
	// Timeout overrides DefaultTimeout when non-zero.
	Timeout time.Duration
	// OutputLimit overrides DefaultOutputLimit when non-zero.
	OutputLimit int
}

// Output runs git and returns trimmed stdout, or an error on non-zero exit.
func Output(ctx context.Context, cmd Command) (string, error) {
	result, err := Run(ctx, cmd)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(result.Stdout)), nil
}

// Run runs git and returns the raw result.
func Run(ctx context.Context, cmd Command) (*exec.Result, error) {
	timeout := cmd.Timeout
	if timeout == 0 {
		timeout = DefaultTimeout
	}
	limit := cmd.OutputLimit
	if limit == 0 {
		limit = DefaultOutputLimit
	}

	filterArgs, err := repositoryFilterOverrides(ctx, cmd.WorktreePath, timeout, limit)
	if err != nil {
		return nil, err
	}

	commandArgs := []string{
		"-C", cmd.WorktreePath,
		"-c", "core.hooksPath=/dev/null",
		"-c", "core.fsmonitor=false",
		"-c", "diff.external=",
		"-c", "credential.helper=",
	}
	commandArgs = append(commandArgs, filterArgs...)
	commandArgs = append(commandArgs, cmd.Args...)

	result, err := exec.Run(ctx, exec.Command{
		Name:        "git",
		Args:        commandArgs,
		Env:         ControlledEnvironment(),
		Timeout:     timeout,
		OutputLimit: limit,
	})
	if err != nil {
		return nil, err
	}
	if result.ExitCode != 0 {
		return result, fmt.Errorf("git exited %d: %s", result.ExitCode, strings.TrimSpace(string(result.Stderr)))
	}
	return result, nil
}

// ControlledEnvironment returns a sanitized environment for Git invocations.
// It strips all GIT_* overrides and other variables that could redirect or
// intercept Git's behavior when running inside an Agent-controlled repository.
func ControlledEnvironment() []string {
	// Variables that must be scrubbed even if prefixed with something other
	// than GIT_ or that are not GIT_ but still affect Git behavior.
	additionalScrub := map[string]struct{}{
		"SSH_AUTH_SOCK":   {},
		"SSH_ASKPASS":     {},
		"GIT_SSH_COMMAND": {},
		"GIT_ASKPASS":     {},
	}

	env := make([]string, 0, len(os.Environ())+4)
	for _, entry := range os.Environ() {
		name, _, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		if strings.HasPrefix(name, "GIT_") {
			continue
		}
		if _, scrub := additionalScrub[name]; scrub {
			continue
		}
		env = append(env, entry)
	}
	env = append(env,
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_SYSTEM=/dev/null",
		"GIT_TERMINAL_PROMPT=0",
		"GIT_ASKPASS=",
	)
	return env
}

// repositoryFilterOverrides detects any configured filter drivers in the
// repository and overrides them to /bin/cat so they cannot execute arbitrary
// commands. This mirrors the logic in internal/pipeline/review/git.go.
func repositoryFilterOverrides(ctx context.Context, worktreePath string, timeout time.Duration, limit int) ([]string, error) {
	result, err := exec.Run(ctx, exec.Command{
		Name: "git",
		Args: []string{
			"-C", worktreePath,
			"config", "--local", "--name-only", "--get-regexp",
			`^filter\..+\.(clean|process|smudge)$`,
		},
		Env:         ControlledEnvironment(),
		Timeout:     timeout,
		OutputLimit: limit,
	})
	if err != nil {
		return nil, err
	}
	if result.ExitCode == 1 {
		// git config exits 1 when no matching keys exist.
		return nil, nil
	}
	if result.ExitCode != 0 {
		return nil, fmt.Errorf("safegit: inspect filter config: git exited %d: %s", result.ExitCode, strings.TrimSpace(string(result.Stderr)))
	}
	if strings.Contains(string(result.Stdout), "[output truncated]") {
		return nil, fmt.Errorf("safegit: inspect filter config: output exceeded %d bytes", limit)
	}

	drivers := make(map[string]struct{})
	for key := range strings.FieldsSeq(string(result.Stdout)) {
		prefix := strings.TrimPrefix(key, "filter.")
		dot := strings.LastIndexByte(prefix, '.')
		if dot <= 0 {
			return nil, fmt.Errorf("safegit: invalid filter key %q", key)
		}
		switch prefix[dot+1:] {
		case "clean", "process", "smudge":
			drivers[prefix[:dot]] = struct{}{}
		default:
			return nil, fmt.Errorf("safegit: invalid filter key %q", key)
		}
	}

	names := make([]string, 0, len(drivers))
	for name := range drivers {
		names = append(names, name)
	}
	sort.Strings(names)

	overrides := make([]string, 0, len(names)*8)
	for _, name := range names {
		p := "filter." + name + "."
		overrides = append(overrides,
			"-c", p+"clean=/bin/cat",
			"-c", p+"smudge=/bin/cat",
			"-c", p+"process=",
			"-c", p+"required=false",
		)
	}
	return overrides, nil
}
