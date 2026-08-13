// Package test is stage 4 of made's pipeline (Intent -> Rebase -> Review ->
// Test -> ...): it runs the effective config's configured test command inside
// the gate worktree via internal/exec and captures full stdout/stderr as
// evidence regardless of outcome. Remote CI owns full regression, so this
// stage runs exactly the configured command - never a broader default suite.
package test

import (
	"context"
	"fmt"
	"strings"

	"github.com/douglasjarquin/made/internal/evidence"
	"github.com/douglasjarquin/made/internal/exec"
)

type Result struct {
	OK       bool
	Message  string
	ExitCode int
}

// Run's error return is reserved for infrastructure failures (empty
// testCommand, the command failing to start, evidence write failure); a
// non-zero exit from the configured test command is a normal outcome
// reported via Result.OK, not an error.
func Run(ctx context.Context, worktreePath, runID string, testCommand []string, store evidence.Store) (Result, error) {
	if len(testCommand) == 0 {
		return Result{}, fmt.Errorf("test: no test command configured")
	}

	res, err := exec.Run(ctx, exec.Command{
		Name: testCommand[0],
		Args: testCommand[1:],
		Dir:  worktreePath,
	})
	if err != nil {
		return Result{}, fmt.Errorf("test: run %q: %w", strings.Join(testCommand, " "), err)
	}

	// Evidence must be written before Result is returned regardless of pass
	// or fail, so a blocked pipeline still leaves a durable record of what
	// the test command produced.
	if evErr := store.WriteEvidence(runID, map[string][]byte{
		"stdout.log": res.Stdout,
		"stderr.log": res.Stderr,
	}); evErr != nil {
		return Result{}, fmt.Errorf("test: write evidence: %w", evErr)
	}

	if res.ExitCode != 0 {
		return Result{
			OK:       false,
			Message:  fmt.Sprintf("test command %q failed with exit code %d", strings.Join(testCommand, " "), res.ExitCode),
			ExitCode: res.ExitCode,
		}, nil
	}

	return Result{
		OK:       true,
		Message:  fmt.Sprintf("test command %q passed", strings.Join(testCommand, " ")),
		ExitCode: 0,
	}, nil
}
