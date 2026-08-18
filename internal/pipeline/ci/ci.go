package ci

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/douglasjarquin/made/internal/github"
)

const defaultPollInterval = 2 * time.Second

type Result struct {
	OK              bool
	Message         string
	RerunRoundsUsed int
	FailureEvidence []FailureEvidence
}

func Run(ctx context.Context, ghClient *github.Client, prURL string, scope github.CheckScope, rerunBudget int, pollInterval time.Duration) (Result, error) {
	if ghClient == nil {
		return Result{}, fmt.Errorf("ci: ghClient must not be nil")
	}
	if strings.TrimSpace(prURL) == "" {
		return Result{}, fmt.Errorf("ci: prURL must not be empty")
	}
	if scope == "" {
		scope = github.CheckScopeRequired
	}
	if !scope.Valid() {
		return Result{}, fmt.Errorf("ci: unsupported check scope %q", scope)
	}
	if rerunBudget < 0 {
		return Result{}, fmt.Errorf("ci: rerunBudget must not be negative")
	}
	if pollInterval <= 0 {
		pollInterval = defaultPollInterval
	}

	rounds := 0
	for {
		checks, err := ghClient.PRChecks(ctx, prURL, scope)
		if err != nil {
			if ctx.Err() != nil {
				return Result{OK: false, Message: ctx.Err().Error(), RerunRoundsUsed: rounds}, nil
			}
			return Result{}, err
		}
		if len(checks.Checks) == 0 {
			return Result{OK: false, Message: fmt.Sprintf("no applicable %s checks for %s", scope, prURL), RerunRoundsUsed: rounds}, nil
		}

		pending, failures := terminalFailures(checks.Checks)
		if pending {
			if err := waitForPoll(ctx, pollInterval); err != nil {
				return Result{OK: false, Message: err.Error(), RerunRoundsUsed: rounds}, nil
			}
			continue
		}
		if len(failures) == 0 {
			return Result{
				OK:              true,
				Message:         fmt.Sprintf("checks passed for %s after %d rerun round(s)", prURL, rounds),
				RerunRoundsUsed: rounds,
			}, nil
		}

		if rounds >= rerunBudget {
			logs, err := fetchFailureLogs(ctx, ghClient, failures)
			if err != nil {
				if ctx.Err() != nil {
					return Result{OK: false, Message: ctx.Err().Error(), RerunRoundsUsed: rounds}, nil
				}
				return Result{}, err
			}
			evidence := collectFailureEvidence(failures, logs)
			return Result{
				OK:              false,
				Message:         formatFailureMessage(prURL, rounds, rerunBudget, evidence),
				RerunRoundsUsed: rounds,
				FailureEvidence: evidence,
			}, nil
		}

		runIDs := rerunnableRunIDs(failures)
		if len(runIDs) == 0 {
			evidence := collectFailureEvidence(failures, nil)
			return Result{
				OK:              false,
				Message:         formatFailureMessage(prURL, rounds, rerunBudget, evidence),
				RerunRoundsUsed: rounds,
				FailureEvidence: evidence,
			}, nil
		}
		for _, runID := range runIDs {
			if err := ghClient.RerunCheck(ctx, runID); err != nil {
				if ctx.Err() != nil {
					return Result{OK: false, Message: ctx.Err().Error(), RerunRoundsUsed: rounds}, nil
				}
				return Result{}, err
			}
		}
		rounds++

		if err := waitForPoll(ctx, pollInterval); err != nil {
			return Result{OK: false, Message: err.Error(), RerunRoundsUsed: rounds}, nil
		}
	}
}

func fetchFailureLogs(ctx context.Context, ghClient *github.Client, failures []github.CheckResult) (map[string]string, error) {
	logs := make(map[string]string)
	for index, runID := range rerunnableRunIDs(failures) {
		if index >= maxFailureLogRuns {
			logs[runID] = omittedFailureLog
			continue
		}
		excerpt, err := ghClient.CheckLogs(ctx, runID)
		if err != nil {
			return nil, err
		}
		logs[runID] = excerpt
	}
	return logs, nil
}

func waitForPoll(ctx context.Context, interval time.Duration) error {
	timer := time.NewTimer(interval)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
