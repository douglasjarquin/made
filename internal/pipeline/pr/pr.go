// Package pr is stage 8 of made's pipeline (Intent -> Rebase -> Review ->
// Test -> Document -> Lint -> Push -> PR -> ...): after a successful Push it
// opens a GitHub pull request for the pushed branch, embedding a reference
// to the run's evidence in the PR body, and stops there.
//
// This package must never gain the ability to merge a pull request, under
// any configuration - not a flag, not a default, not ever. A downstream
// consumer (consigliere) treats its own delivery-mode flag as the sole
// owner of push/PR/merge behavior for a task; if made's unattended pipeline
// could merge on its own, it would silently bypass that human merge
// authority. The guarantee here is structural rather than a runtime check:
// internal/github.Client exposes no merge-capable method for this package
// to call in the first place (verified by no_merge_test.go), so there is no
// merge call to gate behind a flag and no boolean that could accidentally
// flip one on.
package pr

import (
	"context"
	"fmt"
	"strings"

	"github.com/douglasjarquin/made/internal/github"
)

type Options struct {
	Title       string
	Base        string
	Head        string
	EvidenceRef string
}

type Result struct {
	OK      bool
	Message string
	PRURL   string
}

func Run(ctx context.Context, ghClient *github.Client, opts Options) (Result, error) {
	if ghClient == nil {
		return Result{}, fmt.Errorf("pr: ghClient must not be nil")
	}
	if strings.TrimSpace(opts.Title) == "" {
		return Result{}, fmt.Errorf("pr: Title must not be empty")
	}
	if strings.TrimSpace(opts.Base) == "" {
		return Result{}, fmt.Errorf("pr: Base must not be empty")
	}
	if strings.TrimSpace(opts.Head) == "" {
		return Result{}, fmt.Errorf("pr: Head must not be empty")
	}
	if strings.TrimSpace(opts.EvidenceRef) == "" {
		return Result{}, fmt.Errorf("pr: EvidenceRef must not be empty")
	}

	url, err := ghClient.CreatePR(ctx, github.CreatePROptions{
		Title: opts.Title,
		Body:  body(opts.EvidenceRef),
		Base:  opts.Base,
		Head:  opts.Head,
	})
	if err != nil {
		return Result{}, fmt.Errorf("pr: GitHub API failure: %w", err)
	}

	return Result{
		OK:      true,
		Message: fmt.Sprintf("opened PR %s", url),
		PRURL:   url,
	}, nil
}

func body(evidenceRef string) string {
	return fmt.Sprintf("Evidence: %s\n", evidenceRef)
}
