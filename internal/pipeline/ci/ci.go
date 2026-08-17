// Package ci is stage 9 of made's pipeline (Intent -> Rebase -> Review ->
// Test -> Document -> Lint -> Push -> PR -> CI): after a PR is opened it
// polls the PR's checks and, within a hard rerun budget, auto-reruns
// failures that might be transient (flaky infra, a network blip in CI)
// before giving up and reporting a final failure with a log excerpt.
package ci

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/douglasjarquin/made/internal/github"
)

const (
	defaultPollInterval = 2 * time.Second
)

type Result struct {
	OK         bool
	Message    string
	RerunsUsed int
	LogExcerpt string
}

// Run's error return is reserved for infrastructure/configuration failures
// (a nil client, missing PR URL, negative budget); a check that fails on
// GitHub - even after exhausting the rerun budget - is a normal outcome
// reported via Result.OK, not an error, following the pr stage's convention
// (internal/pipeline/pr).
//
// rerunBudget is a hard cap on auto-reruns: without one, a genuinely broken
// (non-transient) check would rerun forever, burning CI minutes and GitHub
// API quota. pollInterval controls the wait between status checks; pass 0
// to use a production-sized default, or a short duration in tests.
func Run(ctx context.Context, ghClient *github.Client, prURL string, rerunBudget int, pollInterval time.Duration) (Result, error) {
	if ghClient == nil {
		return Result{}, fmt.Errorf("ci: ghClient must not be nil")
	}
	if strings.TrimSpace(prURL) == "" {
		return Result{}, fmt.Errorf("ci: prURL must not be empty")
	}
	if rerunBudget < 0 {
		return Result{}, fmt.Errorf("ci: rerunBudget must not be negative")
	}
	if pollInterval <= 0 {
		pollInterval = defaultPollInterval
	}

	reruns := 0
	for {
		checks, err := ghClient.Checks(ctx, prURL)
		if err != nil {
			return Result{}, err
		}
		if len(checks) == 0 {
			return Result{}, fmt.Errorf("ci: GitHub returned no checks for %s", prURL)
		}
		allPassed := true
		var failing *github.Check
		pending := false
		for i := range checks {
			check := checks[i]
			if checkPassed(check) {
				continue
			}
			allPassed = false
			if checkPending(check) {
				pending = true
				continue
			}
			if failing == nil {
				failing = &check
			}
		}
		if allPassed {
			return Result{
				OK:         true,
				Message:    fmt.Sprintf("checks passed for %s after %d rerun(s)", prURL, reruns),
				RerunsUsed: reruns,
			}, nil
		}
		if pending && failing == nil {
			select {
			case <-ctx.Done():
				return Result{}, ctx.Err()
			case <-time.After(pollInterval):
			}
			continue
		}
		if failing == nil {
			return Result{}, fmt.Errorf("ci: check state was neither passing, pending, nor failing")
		}

		if reruns >= rerunBudget {
			excerpt, logErr := ghClient.CheckLogs(ctx, failing.WorkflowRunID)
			if logErr != nil {
				return Result{}, logErr
			}
			return Result{
				OK:         false,
				Message:    fmt.Sprintf("check %s still failing for %s after exhausting rerun budget (%d)", failing.Name, prURL, rerunBudget),
				RerunsUsed: reruns,
				LogExcerpt: excerpt,
			}, nil
		}

		if err := ghClient.RerunCheck(ctx, failing.WorkflowRunID); err != nil {
			return Result{}, err
		}
		reruns++

		select {
		case <-ctx.Done():
			return Result{}, ctx.Err()
		case <-time.After(pollInterval):
		}
	}
}

func checkPassed(check github.Check) bool {
	status := strings.ToUpper(strings.TrimSpace(check.Status))
	conclusion := strings.ToUpper(strings.TrimSpace(check.Conclusion))
	return (status == "SUCCESS" || status == "COMPLETED") && (conclusion == "SUCCESS" || conclusion == "SUCCESSFUL" || conclusion == "NEUTRAL")
}

func checkPending(check github.Check) bool {
	status := strings.ToUpper(strings.TrimSpace(check.Status))
	conclusion := strings.TrimSpace(check.Conclusion)
	return conclusion == "" || status == "QUEUED" || status == "IN_PROGRESS" || status == "PENDING"
}
