// Package review is stage 3 of made's pipeline (Intent -> Rebase -> Review ->
// ...): it spawns the configured Codex agent via internal/agent
// against the diff in a gate worktree, applies auto-fixable findings as new
// commits, and queues ask-user/blocking findings in Result.PendingFindings so
// a later human-approval stage (Task 22) can act on them - findings are never
// silently applied or silently dropped.
package review

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/douglasjarquin/made/internal/agent"
	"github.com/douglasjarquin/made/internal/evidence"
)

type Options struct {
	BinaryPath         string
	ExtraEnv           []string
	Timeout            time.Duration
	BaseBranch         string
	CandidateOutputSHA string
	Evidence           evidence.Store
	EvidenceRunID      string
}

type Result struct {
	OK              bool
	Message         string
	Findings        []agent.Finding
	AutoFixed       []string
	PreFixSHAs      []string
	PostFixSHAs     []string
	PendingFindings []agent.Finding
}

// Run's error return is reserved for infrastructure failures (agent spawn
// failure, unparseable agent output, a scripted patch that fails to apply,
// etc); ask-user and blocking findings are normal outcomes reported via
// Result, not errors.
func Run(ctx context.Context, worktreePath string, agentKind agent.Kind, opts Options) (Result, error) {
	task, err := resolveReviewTask(ctx, worktreePath, opts)
	if err != nil {
		return Result{}, fmt.Errorf("review: build exact-diff task: %w", err)
	}
	beforeStatus, err := gitOutput(ctx, worktreePath, "status", "--porcelain", "--untracked-files=all")
	if err != nil {
		return Result{}, fmt.Errorf("review: inspect worktree before agent: %w", err)
	}
	spawned, err := agent.SpawnWithEvidence(ctx, agentKind, agent.SpawnParams{
		WorktreePath:   worktreePath,
		BinaryPath:     opts.BinaryPath,
		ExtraEnv:       opts.ExtraEnv,
		Task:           task.Text,
		TrustedBaseSHA: task.Contract.TrustedBaseSHA,
		Timeout:        opts.Timeout,
	})
	if err != nil {
		return Result{}, fmt.Errorf("review: spawn %s: %w", agentKind, err)
	}
	afterStatus, err := gitOutput(ctx, worktreePath, "status", "--porcelain", "--untracked-files=all")
	if err != nil {
		return Result{}, fmt.Errorf("review: inspect worktree after agent: %w", err)
	}
	if beforeStatus != afterStatus {
		return Result{}, fmt.Errorf("review: agent modified worktree")
	}

	var autoFixed []string
	var preFixSHAs []string
	var postFixSHAs []string
	var pending []agent.Finding
	var blockingMessages []string

	for _, finding := range spawned.Findings.Findings {
		switch finding.Kind {
		case agent.FindingAutoFixable:
			preSHA, postSHA, applyErr := applyAutoFix(ctx, worktreePath, finding)
			if applyErr != nil {
				return Result{}, fmt.Errorf("review: apply auto-fix %q: %w", finding.Description, applyErr)
			}
			autoFixed = append(autoFixed, postSHA)
			preFixSHAs = append(preFixSHAs, preSHA)
			postFixSHAs = append(postFixSHAs, postSHA)
		case agent.FindingBlocking:
			blockingMessages = append(blockingMessages, finding.Description)
			pending = append(pending, finding)
		default:
			pending = append(pending, finding)
		}
	}

	result := Result{
		OK:              true,
		Message:         fmt.Sprintf("review passed: %d auto-fix(es) applied, %d finding(s) await human approval", len(autoFixed), len(pending)),
		Findings:        spawned.Findings.Findings,
		AutoFixed:       autoFixed,
		PreFixSHAs:      preFixSHAs,
		PostFixSHAs:     postFixSHAs,
		PendingFindings: pending,
	}
	if len(blockingMessages) > 0 {
		result = Result{
			OK:              false,
			Message:         fmt.Sprintf("review halted by blocking finding(s): %s", strings.Join(blockingMessages, "; ")),
			Findings:        spawned.Findings.Findings,
			AutoFixed:       autoFixed,
			PreFixSHAs:      preFixSHAs,
			PostFixSHAs:     postFixSHAs,
			PendingFindings: pending,
		}
	}
	outputSHA, err := gitOutput(ctx, worktreePath, "rev-parse", "--verify", "HEAD^{commit}")
	if err != nil {
		return Result{}, fmt.Errorf("review: resolve candidate output SHA: %w", err)
	}
	if len(postFixSHAs) > 0 && outputSHA != postFixSHAs[len(postFixSHAs)-1] {
		return Result{}, fmt.Errorf("review: candidate output SHA %q does not match the last auto-fix commit %q", outputSHA, postFixSHAs[len(postFixSHAs)-1])
	}
	if opts.CandidateOutputSHA != "" && opts.CandidateOutputSHA != outputSHA {
		return Result{}, fmt.Errorf("review: supplied candidate output SHA %q does not match resolved HEAD %q", opts.CandidateOutputSHA, outputSHA)
	}
	if err := writeReviewEvidence(ctx, opts, task, spawned.Response, outputSHA); err != nil {
		return Result{}, err
	}
	return result, nil
}
