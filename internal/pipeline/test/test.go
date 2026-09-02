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

// ExtraCommand is an additional command Run executes after the primary
// testCommand succeeds - project issue #33 Phase 2's validation-lane "full"
// commands. Name identifies which lane it came from and names its own
// evidence files (<Name>-stdout.log / <Name>-stderr.log) distinctly from
// the primary command's stdout.log/stderr.log.
type ExtraCommand struct {
	Name string
	Args []string
}

// Run's error return is reserved for infrastructure failures (no command at
// all configured, a command failing to start, evidence write failure); a
// non-zero exit from any configured command is a normal outcome reported
// via Result.OK, not an error.
func Run(ctx context.Context, worktreePath, runID string, testCommand []string, extraCommands []ExtraCommand, store evidence.Store) (Result, error) {
	if len(testCommand) == 0 && len(extraCommands) == 0 {
		return Result{}, fmt.Errorf("test: no test command configured")
	}

	evidenceFiles := map[string][]byte{}
	failResult := Result{}
	failed := false

	if len(testCommand) > 0 {
		res, err := exec.Run(ctx, exec.Command{Name: testCommand[0], Args: testCommand[1:], Dir: worktreePath})
		if err != nil {
			return Result{}, fmt.Errorf("test: run %q: %w", strings.Join(testCommand, " "), err)
		}
		evidenceFiles["stdout.log"] = res.Stdout
		evidenceFiles["stderr.log"] = res.Stderr
		if res.ExitCode != 0 {
			failed = true
			failResult = Result{OK: false, Message: fmt.Sprintf("test command %q failed with exit code %d", strings.Join(testCommand, " "), res.ExitCode), ExitCode: res.ExitCode}
		}
	}

	for _, extra := range extraCommands {
		if failed {
			break
		}
		if len(extra.Args) == 0 {
			continue
		}
		res, err := exec.Run(ctx, exec.Command{Name: extra.Args[0], Args: extra.Args[1:], Dir: worktreePath})
		if err != nil {
			return Result{}, fmt.Errorf("test: run %q (%s): %w", strings.Join(extra.Args, " "), extra.Name, err)
		}
		evidenceFiles[extra.Name+"-stdout.log"] = res.Stdout
		evidenceFiles[extra.Name+"-stderr.log"] = res.Stderr
		if res.ExitCode != 0 {
			failed = true
			failResult = Result{OK: false, Message: fmt.Sprintf("%s: command %q failed with exit code %d", extra.Name, strings.Join(extra.Args, " "), res.ExitCode), ExitCode: res.ExitCode}
		}
	}

	// Evidence must be written before Result is returned regardless of pass
	// or fail, so a blocked pipeline still leaves a durable record of what
	// every command produced.
	var evErr error
	if contextual, ok := store.(evidence.ContextStore); ok {
		evErr = contextual.WriteEvidenceContext(ctx, runID, evidenceFiles)
	} else {
		evErr = store.WriteEvidence(runID, evidenceFiles)
	}
	if evErr != nil {
		return Result{}, fmt.Errorf("test: write evidence: %w", evErr)
	}

	if failed {
		return failResult, nil
	}
	return Result{OK: true, Message: fmt.Sprintf("%d command(s) passed", 1+len(extraCommands)), ExitCode: 0}, nil
}
