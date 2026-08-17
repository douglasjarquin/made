// Package lint is stage 6 of made's pipeline (Intent -> Rebase -> Review ->
// Test -> Document -> Lint -> ...): it runs the effective config's
// configured lint command inside the gate worktree via internal/exec and
// captures full stdout/stderr as evidence regardless of outcome. Unlike the
// Test stage, an unset lint command is not an infrastructure error: Document
// already ran independently in this pipeline design, so a missing lint
// command degrades to a graceful no-op rather than a hard failure.
package lint

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

// Run's error return is reserved for infrastructure failures (the command
// failing to start, evidence write failure); a non-zero exit from the
// configured lint command is a normal outcome reported via Result.OK, not an
// error, and an unset lintCommand is reported as a graceful no-op success.
func Run(ctx context.Context, worktreePath, runID string, lintCommand []string, store evidence.Store) (Result, error) {
	if len(lintCommand) == 0 {
		return Result{
			OK:      true,
			Message: "no lint command configured, skipped",
		}, nil
	}

	res, err := exec.Run(ctx, exec.Command{
		Name: lintCommand[0],
		Args: lintCommand[1:],
		Dir:  worktreePath,
	})
	if err != nil {
		return Result{}, fmt.Errorf("lint: run %q: %w", strings.Join(lintCommand, " "), err)
	}

	evidenceFiles := map[string][]byte{
		"stdout.log": res.Stdout,
		"stderr.log": res.Stderr,
	}
	var evErr error
	if contextual, ok := store.(evidence.ContextStore); ok {
		evErr = contextual.WriteEvidenceContext(ctx, runID, evidenceFiles)
	} else {
		evErr = store.WriteEvidence(runID, evidenceFiles)
	}
	if evErr != nil {
		return Result{}, fmt.Errorf("lint: write evidence: %w", evErr)
	}

	if res.ExitCode != 0 {
		return Result{
			OK:       false,
			Message:  fmt.Sprintf("lint command %q failed with exit code %d", strings.Join(lintCommand, " "), res.ExitCode),
			ExitCode: res.ExitCode,
		}, nil
	}

	return Result{
		OK:       true,
		Message:  fmt.Sprintf("lint command %q passed", strings.Join(lintCommand, " ")),
		ExitCode: 0,
	}, nil
}
