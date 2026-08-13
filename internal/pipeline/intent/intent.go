// Package intent is stage 1 of made's pipeline: it is the chain contract
// later stages (starting with Rebase) build on, so Check's Result shape is
// deliberately minimal and stable.
package intent

import (
	"fmt"
	"os/exec"
	"strings"
)

type Result struct {
	OK      bool
	Message string
}

// Check's error return is reserved for infrastructure failures (repoPath
// unreadable, git missing, etc); a missing/empty Intent trailer is a normal
// outcome reported via Result.OK, not an error.
func Check(repoPath string) (Result, error) {
	message, err := commitMessage(repoPath)
	if err != nil {
		return Result{}, err
	}

	value, err := intentTrailerValue(repoPath, message)
	if err != nil {
		return Result{}, err
	}

	if value == "" {
		return Result{
			OK:      false,
			Message: `missing required Intent: add an "Intent: <summary>" trailer to the branch tip commit message`,
		}, nil
	}

	return Result{
		OK:      true,
		Message: fmt.Sprintf("intent stage passed: %q", value),
	}, nil
}

func commitMessage(repoPath string) (string, error) {
	cmd := exec.Command("git", "-C", repoPath, "log", "-1", "--format=%B", "HEAD")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("intent: read tip commit message: %w", err)
	}
	return string(out), nil
}

func intentTrailerValue(repoPath, message string) (string, error) {
	cmd := exec.Command("git", "-C", repoPath, "interpret-trailers", "--parse")
	cmd.Stdin = strings.NewReader(message)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("intent: parse commit trailers: %w", err)
	}

	for _, line := range strings.Split(string(out), "\n") {
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(key), "Intent") {
			return strings.TrimSpace(value), nil
		}
	}
	return "", nil
}
