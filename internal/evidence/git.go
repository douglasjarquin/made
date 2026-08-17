package evidence

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"

	execpkg "github.com/douglasjarquin/made/internal/exec"
)

func evidenceGitArgs(ctx context.Context, repoPath string, args ...string) ([]string, error) {
	filterArgs, err := evidenceFilterOverrides(ctx, repoPath)
	if err != nil {
		return nil, err
	}
	commandArgs := []string{
		"-C", repoPath,
		"-c", "core.hooksPath=/dev/null",
		"-c", "core.fsmonitor=false",
		"-c", "diff.external=",
	}
	commandArgs = append(commandArgs, filterArgs...)
	return append(commandArgs, args...), nil
}

func evidenceFilterOverrides(ctx context.Context, repoPath string) ([]string, error) {
	result, err := execpkg.Run(ctx, execpkg.Command{
		Name: "git",
		Args: []string{
			"-C", repoPath,
			"config", "--local", "--name-only", "--get-regexp",
			"^filter\\..+\\.(clean|process|smudge)$",
		},
		Env:         controlledEvidenceGitEnvironment(),
		Timeout:     evidenceGitTimeout,
		OutputLimit: evidenceGitOutputCap,
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
		return nil, fmt.Errorf("inspect repository Git filters: output exceeded %d bytes", evidenceGitOutputCap)
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

func controlledEvidenceGitEnvironment(extraEnv ...string) []string {
	env := make([]string, 0, len(os.Environ())+4+len(extraEnv))
	for _, entry := range os.Environ() {
		name, _, ok := strings.Cut(entry, "=")
		if !ok || strings.HasPrefix(name, "GIT_") || name == "SSH_AUTH_SOCK" {
			continue
		}
		env = append(env, entry)
	}
	env = append(env,
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_SYSTEM=/dev/null",
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_TERMINAL_PROMPT=0",
	)
	return append(env, extraEnv...)
}
