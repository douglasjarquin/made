package orchestrator

import (
	"context"
	"fmt"
	"time"

	"github.com/douglasjarquin/made/internal/agent"
	"github.com/douglasjarquin/made/internal/daemon"
	"github.com/douglasjarquin/made/internal/pipeline/ci"
	"github.com/douglasjarquin/made/internal/pipeline/document"
	"github.com/douglasjarquin/made/internal/pipeline/intent"
	"github.com/douglasjarquin/made/internal/pipeline/lint"
	"github.com/douglasjarquin/made/internal/pipeline/pr"
	"github.com/douglasjarquin/made/internal/pipeline/push"
	"github.com/douglasjarquin/made/internal/pipeline/rebase"
	"github.com/douglasjarquin/made/internal/pipeline/review"
	"github.com/douglasjarquin/made/internal/pipeline/test"
)

const (
	stageResultPass = "pass"
	stageResultFail = "fail"

	stageNameIntent   = "intent"
	stageNameRebase   = "rebase"
	stageNameReview   = "review"
	stageNameTest     = "test"
	stageNameDocument = "document"
	stageNameLint     = "lint"
	stageNamePush     = "push"
	stageNamePR       = "pr"
	stageNameCI       = "ci"

	ciStageTimeout = 30 * time.Minute
	ciPollInterval = 10 * time.Second

	prTitle = "made validation"
)

// Options carries the per-run parameters Task 14 will derive for real
// (real PR title, real agent binary resolution, real Document rules from
// config); until then it defaults to the zero value everywhere it is used
// below, and only this package's own tests populate it, to stand in a fake
// agent/gh binary in place of a real one.
type Options struct {
	ReviewOptions review.Options
	DocumentRules []document.Rule
}

// NewWorkFunc builds the real 9-stage chain (Intent -> Rebase -> Review ->
// Test -> Document -> Lint -> Push -> PR -> CI) as the WorkFunc Run/Setup
// (scaffold.go) invokes against a *RunContext. rm and reviewDecisions live in
// internal/daemon and are otherwise unreachable from inside that callback -
// rm records real per-stage results as the run progresses, and
// reviewDecisions is where the ask-user park/resume path blocks and resumes.
func NewWorkFunc(rm *daemon.RunManager, reviewDecisions *daemon.ReviewDecisions, emit func(daemon.Event), runID, defaultBranch, branch string, opts Options) WorkFunc {
	return func(ctx context.Context, rc *RunContext) error {
		c := &chain{
			ctx:             ctx,
			rc:              rc,
			rm:              rm,
			reviewDecisions: reviewDecisions,
			emit:            emit,
			runID:           runID,
			defaultBranch:   defaultBranch,
			branch:          branch,
			opts:            opts,
		}
		return c.run()
	}
}

type chain struct {
	ctx             context.Context
	rc              *RunContext
	rm              *daemon.RunManager
	reviewDecisions *daemon.ReviewDecisions
	emit            func(daemon.Event)

	runID         string
	defaultBranch string
	branch        string
	opts          Options

	stages []daemon.StageResult
}

func (c *chain) run() error {
	if err := c.intentStage(); err != nil {
		return err
	}
	if err := c.rebaseStage(); err != nil {
		return err
	}
	if err := c.reviewStage(); err != nil {
		return err
	}
	if err := c.testStage(); err != nil {
		return err
	}
	if err := c.documentStage(); err != nil {
		return err
	}
	if err := c.lintStage(); err != nil {
		return err
	}
	if err := c.pushStage(); err != nil {
		return err
	}
	prResult, err := c.prStage()
	if err != nil {
		return err
	}
	if err := c.ciStage(prResult.PRURL); err != nil {
		return err
	}

	// A passing CI stage validates the branch and leaves a PR open, but
	// merging it is a human decision made cannot observe - so the run's
	// final status stays RunRunning rather than RunCompleted, with the PR
	// URL surfaced in the message instead of a terminal "done" state.
	message := fmt.Sprintf("all stages passed, PR open, awaiting merge: %s", prResult.PRURL)
	return c.rm.Finish(c.runID, daemon.RunRunning, message)
}

func (c *chain) start(stage string) {
	if c.emit != nil {
		c.emit(daemon.Event{Kind: daemon.EventStageStarted, Stage: stage})
	}
}

func (c *chain) finish(stage, result, message string) {
	c.stages = append(c.stages, daemon.StageResult{Name: stage, Result: result})
	_ = c.rm.UpdateStages(c.runID, append([]daemon.StageResult(nil), c.stages...))
	if c.emit != nil {
		c.emit(daemon.Event{Kind: daemon.EventStageFinished, Stage: stage, Message: message})
	}
}

func stageFailure(stage, message string) error {
	return fmt.Errorf("orchestrator: stage %q failed: %s", stage, message)
}

func (c *chain) intentStage() error {
	c.start(stageNameIntent)
	result, err := intent.Check(c.rc.Worktree.Path)
	if err != nil {
		return err
	}
	if !result.OK {
		c.finish(stageNameIntent, stageResultFail, result.Message)
		return stageFailure(stageNameIntent, result.Message)
	}
	c.finish(stageNameIntent, stageResultPass, result.Message)
	return nil
}

func (c *chain) rebaseStage() error {
	c.start(stageNameRebase)
	result, err := rebase.Run(c.rc.Worktree.Path, c.defaultBranch)
	if err != nil {
		return err
	}
	if !result.OK {
		c.finish(stageNameRebase, stageResultFail, result.Message)
		return stageFailure(stageNameRebase, result.Message)
	}
	c.finish(stageNameRebase, stageResultPass, result.Message)
	return nil
}

func (c *chain) reviewStage() error {
	c.start(stageNameReview)

	agentKind, err := c.rc.Config.AgentKind()
	if err != nil {
		return fmt.Errorf("orchestrator: resolve agent kind: %w", err)
	}

	result, err := review.Run(c.ctx, c.rc.Worktree.Path, agentKind, c.opts.ReviewOptions)
	if err != nil {
		return err
	}
	if !result.OK {
		c.finish(stageNameReview, stageResultFail, result.Message)
		return stageFailure(stageNameReview, result.Message)
	}

	if len(result.PendingFindings) > 0 {
		if err := c.parkForApproval(stageNameReview, findingsToAskUser(stageNameReview, result.PendingFindings)); err != nil {
			return err
		}
	}

	c.finish(stageNameReview, stageResultPass, result.Message)
	return nil
}

func (c *chain) testStage() error {
	c.start(stageNameTest)
	result, err := test.Run(c.ctx, c.rc.Worktree.Path, c.runID, c.rc.Config.TestCommand(), c.rc.Evidence)
	if err != nil {
		return err
	}
	if !result.OK {
		c.finish(stageNameTest, stageResultFail, result.Message)
		return stageFailure(stageNameTest, result.Message)
	}
	c.finish(stageNameTest, stageResultPass, result.Message)
	return nil
}

func (c *chain) documentStage() error {
	c.start(stageNameDocument)
	result, err := document.Run(c.rc.Worktree.Path, c.defaultBranch, c.opts.DocumentRules)
	if err != nil {
		return err
	}
	if !result.OK {
		c.finish(stageNameDocument, stageResultFail, result.Message)
		return stageFailure(stageNameDocument, result.Message)
	}

	if len(result.Findings) > 0 {
		if err := c.parkForApproval(stageNameDocument, findingsToAskUser(stageNameDocument, result.Findings)); err != nil {
			return err
		}
	}

	c.finish(stageNameDocument, stageResultPass, result.Message)
	return nil
}

func (c *chain) lintStage() error {
	c.start(stageNameLint)
	result, err := lint.Run(c.ctx, c.rc.Worktree.Path, c.runID, c.rc.Config.LintCommand(), c.rc.Evidence)
	if err != nil {
		return err
	}
	if !result.OK {
		c.finish(stageNameLint, stageResultFail, result.Message)
		return stageFailure(stageNameLint, result.Message)
	}
	c.finish(stageNameLint, stageResultPass, result.Message)
	return nil
}

func (c *chain) pushStage() error {
	c.start(stageNamePush)
	result, err := push.Run(c.ctx, c.rc.Worktree.Path, "origin", c.branch)
	if err != nil {
		return err
	}
	if !result.OK {
		c.finish(stageNamePush, stageResultFail, result.Message)
		return stageFailure(stageNamePush, result.Message)
	}
	c.finish(stageNamePush, stageResultPass, result.Message)
	return nil
}

func (c *chain) prStage() (pr.Result, error) {
	c.start(stageNamePR)
	result, err := pr.Run(c.ctx, c.rc.GitHub, pr.Options{
		Title:       prTitle,
		Base:        c.defaultBranch,
		Head:        c.branch,
		EvidenceRef: c.runID,
	})
	if err != nil {
		return pr.Result{}, err
	}
	if !result.OK {
		c.finish(stageNamePR, stageResultFail, result.Message)
		return pr.Result{}, stageFailure(stageNamePR, result.Message)
	}
	c.finish(stageNamePR, stageResultPass, result.Message)
	return result, nil
}

func (c *chain) ciStage(prURL string) error {
	c.start(stageNameCI)
	ciCtx, cancel := context.WithTimeout(c.ctx, ciStageTimeout)
	defer cancel()

	result, err := ci.Run(ciCtx, c.rc.GitHub, prURL, c.rc.Config.CI.RerunBudget, ciPollInterval)
	if err != nil {
		return err
	}
	if !result.OK {
		c.finish(stageNameCI, stageResultFail, result.Message)
		return stageFailure(stageNameCI, result.Message)
	}
	c.finish(stageNameCI, stageResultPass, result.Message)
	return nil
}

// parkForApproval records findings and blocks on a single decision per
// stage (findings from the same stage are approved or rejected together,
// not individually) until reviewDecisions resolves it. It is called only
// when a stage's own Result.OK is already true: Review and Document can both
// report success while still carrying findings a human must weigh in on, so
// OK alone is never sufficient to proceed past them.
func (c *chain) parkForApproval(stage string, findings []daemon.AskUserFinding) error {
	_ = c.rm.UpdatePendingFindings(c.runID, findings)
	decision, err := c.reviewDecisions.Wait(c.ctx, c.runID, stage)
	_ = c.rm.UpdatePendingFindings(c.runID, nil)
	if err != nil {
		return fmt.Errorf("orchestrator: wait for %s decision: %w", stage, err)
	}
	if decision == daemon.ReviewRejected {
		c.finish(stage, stageResultFail, "rejected by reviewer")
		return stageFailure(stage, "rejected by reviewer")
	}
	return nil
}

func findingsToAskUser(stage string, findings []agent.Finding) []daemon.AskUserFinding {
	out := make([]daemon.AskUserFinding, len(findings))
	for i, f := range findings {
		out[i] = daemon.AskUserFinding{Stage: stage, Message: f.Description}
	}
	return out
}
