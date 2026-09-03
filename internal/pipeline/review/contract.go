package review

import (
	"context"
	"fmt"
	"strings"

	"github.com/douglasjarquin/made/internal/agent"
)

func resolveReviewTask(ctx context.Context, worktreePath string, opts Options) (agent.ReviewTask, error) {
	candidateSHA, err := gitOutput(ctx, worktreePath, "rev-parse", "--verify", "HEAD^{commit}")
	if err != nil {
		return agent.ReviewTask{}, fmt.Errorf("resolve candidate input SHA: %w", err)
	}
	baseBranch := strings.TrimSpace(opts.BaseBranch)
	if baseBranch == "" {
		baseBranch = "HEAD"
	}
	if strings.HasPrefix(baseBranch, "-") || strings.ContainsAny(baseBranch, "\r\n") {
		return agent.ReviewTask{}, fmt.Errorf("trusted base branch %q is not a valid Git ref", baseBranch)
	}
	baseSHA := candidateSHA
	if baseBranch != "HEAD" {
		baseSHA, err = gitOutput(ctx, worktreePath, "rev-parse", "--verify", "--end-of-options", baseBranch+"^{commit}")
		if err != nil {
			return agent.ReviewTask{}, fmt.Errorf("resolve trusted base %q: %w", baseBranch, err)
		}
	}
	reviewInput := agent.ReviewInput{
		TrustedBaseBranch:  baseBranch,
		TrustedBaseSHA:     baseSHA,
		CandidateInputSHA:  candidateSHA,
		CandidateOutputSHA: opts.CandidateOutputSHA,
		Guides:             opts.Guides,
	}
	// Managed-validation mode requires strict structural identity for findings.
	if opts.ReportOnly {
		return agent.NewManagedReviewTask(reviewInput)
	}
	return agent.NewReviewTask(reviewInput)
}
