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

	// passingMergeState is the gh pr view mergeStateStatus value that means
	// "all checks passed and the PR is clear to proceed". internal/github's
	// Client exposes no separate check-listing endpoint, so this stage
	// treats PR mergeability status as its check-status signal; any other
	// state is treated as a (possibly transient) check failure.
	passingMergeState = "CLEAN"
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
		state, err := ghClient.MergeableState(ctx, prURL)
		if err != nil {
			return Result{OK: false, Message: err.Error(), RerunsUsed: reruns}, nil
		}
		if state == passingMergeState {
			return Result{
				OK:         true,
				Message:    fmt.Sprintf("checks passed for %s after %d rerun(s)", prURL, reruns),
				RerunsUsed: reruns,
			}, nil
		}

		if reruns >= rerunBudget {
			excerpt, logErr := ghClient.CheckLogs(ctx, prURL)
			if logErr != nil {
				excerpt = fmt.Sprintf("(failed to fetch check logs: %s)", logErr.Error())
			}
			return Result{
				OK:         false,
				Message:    fmt.Sprintf("checks still failing (%s) for %s after exhausting rerun budget (%d)", state, prURL, rerunBudget),
				RerunsUsed: reruns,
				LogExcerpt: excerpt,
			}, nil
		}

		if err := ghClient.RerunCheck(ctx, prURL); err != nil {
			return Result{OK: false, Message: err.Error(), RerunsUsed: reruns}, nil
		}
		reruns++

		select {
		case <-ctx.Done():
			return Result{OK: false, Message: ctx.Err().Error(), RerunsUsed: reruns}, nil
		case <-time.After(pollInterval):
		}
	}
}
