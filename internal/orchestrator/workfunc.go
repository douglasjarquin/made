package orchestrator

import (
	"context"
	"fmt"
	"time"

	"github.com/douglasjarquin/made/internal/agent"
	"github.com/douglasjarquin/made/internal/daemon"
	"github.com/douglasjarquin/made/internal/evidence"
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

	ciPollInterval = 10 * time.Second

	pushRemoteName = "origin"
)

// Options carries the per-run parameters that come from outside the pushed
// commit/config itself (agent binary resolution); it defaults to the zero
// value in production and only this package's own tests populate it, to
// stand in a fake agent binary in place of a real one. Document rules come
// from the resolved RunContext.Config instead, since Config itself is only
// resolved at Setup time, after a WorkFunc closure is already built.
type Options struct {
	ReviewOptions      review.Options
	CandidateOutputSHA string
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

	// pushed flips true the moment pushStage lands the branch on the real
	// remote. made has no authority to revert, force-push, or delete a
	// branch already on a real remote - that destructive action is outside
	// its gate/worktree - so any stage failure after this point needs a
	// failure message that says so explicitly rather than the generic
	// per-stage wording, since a human now has to notice and clean up.
	pushed bool

	// evidencePublishedSHA is the exact commit PublishEvidenceSHA put on
	// origin during pushStage, captured once so prStage's link can never
	// point at a commit a later, concurrent run advanced the branch to but
	// never pushed.
	evidencePublishedSHA string
}

func (c *chain) run() error {
	if err := c.runStage(stageNameIntent, c.intentStage); err != nil {
		return err
	}
	if err := c.runStage(stageNameRebase, c.rebaseStage); err != nil {
		return err
	}
	if err := c.runStage(stageNameReview, c.reviewStage); err != nil {
		return err
	}
	if err := c.runStage(stageNameTest, c.testStage); err != nil {
		return err
	}
	if err := c.runStage(stageNameDocument, c.documentStage); err != nil {
		return err
	}
	if err := c.runStage(stageNameLint, c.lintStage); err != nil {
		return err
	}
	if err := c.requireDeliveryStages(); err != nil {
		return err
	}
	if err := c.runStage(stageNamePush, c.pushStage); err != nil {
		return err
	}
	var prResult pr.Result
	var err error
	if c.rc.Config.StageResult(stageNamePR) == "skipped" {
		if err := c.finish(stageNamePR, "skipped", "stage disabled"); err != nil {
			return err
		}
	} else {
		prResult, err = c.prStage()
	}
	if err != nil {
		return err
	}
	if err := c.runCIStage(prResult.PRURL); err != nil {
		return err
	}

	// A passing CI stage validates the branch and leaves a PR open, but
	// merging it is a human decision made cannot observe - so the run's
	// final status stays RunAwaitingMerge rather than RunSucceeded, with the PR
	// URL surfaced in the message instead of a terminal "done" state.
	message := fmt.Sprintf("all stages passed, PR open, awaiting merge: %s", prResult.PRURL)
	return c.rm.Finish(c.runID, daemon.RunAwaitingMerge, message)
}

func (c *chain) requireDeliveryStages() error {
	for _, name := range []string{
		stageNameIntent, stageNameRebase, stageNameReview, stageNameTest,
		stageNameDocument, stageNameLint, stageNamePush, stageNamePR, stageNameCI,
	} {
		if c.rc.Config.StageResult(name) != "skipped" {
			continue
		}
		if !c.rc.Config.StageRequired(name) {
			continue
		}
		if err := c.finish(name, "skipped", "stage disabled"); err != nil {
			return err
		}
		return fmt.Errorf("orchestrator: refusing delivery because required stage %q is disabled", name)
	}
	return nil
}

func (c *chain) runStage(name string, stage func() error) error {
	if c.rc.Config.StageResult(name) == "skipped" {
		return c.finish(name, "skipped", "stage disabled")
	}
	stageCtx, cancel := context.WithTimeout(c.ctx, c.rc.Config.StageTimeout(name))
	previous := c.ctx
	c.ctx = stageCtx
	err := stage()
	c.ctx = previous
	cancel()
	return err
}

func (c *chain) runCIStage(prURL string) error {
	if c.rc.Config.StageResult(stageNameCI) == "skipped" || prURL == "" {
		return c.finish(stageNameCI, "skipped", "stage disabled")
	}
	return c.ciStage(prURL)
}

func (c *chain) start(stage string) {
	if c.emit != nil {
		c.emit(daemon.Event{Kind: daemon.EventStageStarted, Stage: stage})
	}
}

func (c *chain) finish(stage, result, message string) error {
	c.stages = append(c.stages, daemon.StageResult{Name: stage, Result: result})
	if err := c.rm.UpdateStages(c.runID, append([]daemon.StageResult(nil), c.stages...)); err != nil {
		return err
	}
	if c.emit != nil {
		c.emit(daemon.Event{Kind: daemon.EventStageFinished, Stage: stage, Message: message})
	}
	return nil
}

func (c *chain) stageFailure(stage, message string) error {
	if !c.pushed {
		return fmt.Errorf("orchestrator: stage %q failed: %s", stage, message)
	}
	label := stage
	switch stage {
	case stageNamePR:
		label = "PR creation"
	case stageNameCI:
		label = "CI"
	}
	return fmt.Errorf("orchestrator: push succeeded (branch %s now on %s), but %s failed: %s - the branch is live on the real remote, no automatic action taken", c.branch, pushRemoteName, label, message)
}

func (c *chain) intentStage() error {
	c.start(stageNameIntent)
	result, err := intent.CheckContext(c.ctx, c.rc.Worktree.Path)
	if err != nil {
		return err
	}
	if !result.OK {
		if err := c.finish(stageNameIntent, stageResultFail, result.Message); err != nil {
			return err
		}
		return c.stageFailure(stageNameIntent, result.Message)
	}
	return c.finish(stageNameIntent, stageResultPass, result.Message)
}

func (c *chain) rebaseStage() error {
	c.start(stageNameRebase)
	result, err := rebase.RunContext(c.ctx, c.rc.Worktree.Path, c.defaultBranch)
	if err != nil {
		return err
	}
	if !result.OK {
		if err := c.finish(stageNameRebase, stageResultFail, result.Message); err != nil {
			return err
		}
		return c.stageFailure(stageNameRebase, result.Message)
	}
	return c.finish(stageNameRebase, stageResultPass, result.Message)
}

func (c *chain) reviewStage() error {
	c.start(stageNameReview)

	agentKind, err := c.rc.Config.AgentKind()
	if err != nil {
		return fmt.Errorf("orchestrator: resolve agent kind: %w", err)
	}

	reviewOptions := c.opts.ReviewOptions
	reviewOptions.BaseBranch = c.defaultBranch
	reviewOptions.CandidateOutputSHA = c.opts.CandidateOutputSHA
	reviewOptions.Evidence = c.rc.Evidence
	reviewOptions.EvidenceRunID = c.runID
	result, err := review.Run(c.ctx, c.rc.Worktree.Path, agentKind, reviewOptions)
	if err != nil {
		return err
	}
	durableFindings := make([]daemon.RunFinding, 0, len(result.Findings))
	autoFixIndex := 0
	for _, finding := range result.Findings {
		record := daemon.RunFinding{Stage: stageNameReview, Kind: string(finding.Kind), Message: finding.Description, Paths: append([]string(nil), finding.Paths...)}
		if finding.Kind == agent.FindingAutoFixable && autoFixIndex < len(result.PreFixSHAs) {
			record.PreFixSHA = result.PreFixSHAs[autoFixIndex]
			record.PostFixSHA = result.PostFixSHAs[autoFixIndex]
			autoFixIndex++
		}
		durableFindings = append(durableFindings, record)
	}
	if err := c.rm.AddFindings(c.runID, durableFindings); err != nil {
		return err
	}
	if !result.OK {
		if err := c.finish(stageNameReview, stageResultFail, result.Message); err != nil {
			return err
		}
		return c.stageFailure(stageNameReview, result.Message)
	}

	if len(result.PendingFindings) > 0 {
		if err := c.parkForApproval(stageNameReview, findingsToAskUser(stageNameReview, result.PendingFindings)); err != nil {
			return err
		}
	}

	return c.finish(stageNameReview, stageResultPass, result.Message)
}

func (c *chain) testStage() error {
	c.start(stageNameTest)
	result, err := test.Run(c.ctx, c.rc.Worktree.Path, c.runID, c.rc.Config.TestCommand(), c.rc.Evidence)
	if err != nil {
		return err
	}
	if !result.OK {
		if err := c.finish(stageNameTest, stageResultFail, result.Message); err != nil {
			return err
		}
		return c.stageFailure(stageNameTest, result.Message)
	}
	return c.finish(stageNameTest, stageResultPass, result.Message)
}

func (c *chain) documentStage() error {
	c.start(stageNameDocument)
	result, err := document.RunContext(c.ctx, c.rc.Worktree.Path, c.defaultBranch, deriveDocumentRules(c.rc.Config))
	if err != nil {
		return err
	}
	durableFindings := make([]daemon.RunFinding, 0, len(result.Findings))
	for _, finding := range result.Findings {
		durableFindings = append(durableFindings, daemon.RunFinding{Stage: stageNameDocument, Kind: string(finding.Kind), Message: finding.Description, Paths: append([]string(nil), finding.Paths...)})
	}
	if err := c.rm.AddFindings(c.runID, durableFindings); err != nil {
		return err
	}
	if !result.OK {
		if err := c.finish(stageNameDocument, stageResultFail, result.Message); err != nil {
			return err
		}
		return c.stageFailure(stageNameDocument, result.Message)
	}

	if len(result.Findings) > 0 {
		if err := c.parkForApproval(stageNameDocument, findingsToAskUser(stageNameDocument, result.Findings)); err != nil {
			return err
		}
	}

	return c.finish(stageNameDocument, stageResultPass, result.Message)
}

func (c *chain) lintStage() error {
	c.start(stageNameLint)
	result, err := lint.Run(c.ctx, c.rc.Worktree.Path, c.runID, c.rc.Config.LintCommand(), c.rc.Evidence)
	if err != nil {
		return err
	}
	if !result.OK {
		if err := c.finish(stageNameLint, stageResultFail, result.Message); err != nil {
			return err
		}
		return c.stageFailure(stageNameLint, result.Message)
	}
	return c.finish(stageNameLint, stageResultPass, result.Message)
}

func (c *chain) pushStage() error {
	c.start(stageNamePush)
	if orphan, ok := c.rc.Evidence.(*evidence.OrphanBranchStore); ok {
		sha, publishErr := orphan.PublishEvidenceSHA(c.ctx, c.runID)
		if publishErr != nil {
			if finishErr := c.finish(stageNamePush, stageResultFail, publishErr.Error()); finishErr != nil {
				return finishErr
			}
			return c.stageFailure(stageNamePush, publishErr.Error())
		}
		c.evidencePublishedSHA = sha
	} else if publisher, ok := c.rc.Evidence.(evidence.Publisher); ok {
		var publishErr error
		if contextual, contextOK := c.rc.Evidence.(evidence.ContextPublisher); contextOK {
			publishErr = contextual.PublishEvidenceContext(c.ctx, c.runID)
		} else {
			publishErr = publisher.PublishEvidence(c.runID)
		}
		if publishErr != nil {
			if finishErr := c.finish(stageNamePush, stageResultFail, publishErr.Error()); finishErr != nil {
				return finishErr
			}
			return c.stageFailure(stageNamePush, publishErr.Error())
		}
	}
	outputSHA, err := deriveOutputSHA(c.rc.Worktree.Path)
	if err != nil {
		if finishErr := c.finish(stageNamePush, stageResultFail, err.Error()); finishErr != nil {
			return finishErr
		}
		return c.stageFailure(stageNamePush, err.Error())
	}
	if err := c.rm.SetOutputSHA(c.runID, outputSHA); err != nil {
		return err
	}
	result, err := push.Run(c.ctx, c.rc.Worktree.Path, pushRemoteName, c.branch)
	if err != nil {
		return err
	}
	if !result.OK {
		if err := c.finish(stageNamePush, stageResultFail, result.Message); err != nil {
			return err
		}
		return c.stageFailure(stageNamePush, result.Message)
	}
	c.pushed = true
	return c.finish(stageNamePush, stageResultPass, result.Message)
}

func (c *chain) prStage() (pr.Result, error) {
	c.start(stageNamePR)
	title, err := derivePRTitle(c.rc.Worktree.Path)
	if err != nil {
		return pr.Result{}, fmt.Errorf("orchestrator: derive PR title: %w", err)
	}

	result, err := pr.Run(c.ctx, c.rc.GitHub, pr.Options{
		Title:       title,
		Base:        c.defaultBranch,
		Head:        c.branch,
		EvidenceRef: deriveEvidenceRef(c.rc.Evidence, c.runID),
		EvidenceURL: deriveEvidenceURL(c.ctx, c.rc.Worktree.Path, c.evidencePublishedSHA, c.runID),
		RunID:       c.runID,
	})
	if err != nil {
		if finishErr := c.finish(stageNamePR, stageResultFail, err.Error()); finishErr != nil {
			return pr.Result{}, finishErr
		}
		return pr.Result{}, c.stageFailure(stageNamePR, err.Error())
	}
	if !result.OK {
		if err := c.finish(stageNamePR, stageResultFail, result.Message); err != nil {
			return pr.Result{}, err
		}
		return pr.Result{}, c.stageFailure(stageNamePR, result.Message)
	}
	if err := c.rm.SetPRURL(c.runID, result.PRURL); err != nil {
		return pr.Result{}, err
	}
	if err := c.finish(stageNamePR, stageResultPass, result.Message); err != nil {
		return pr.Result{}, err
	}
	return result, nil
}

func (c *chain) ciStage(prURL string) error {
	c.start(stageNameCI)
	ciCtx, cancel := context.WithTimeout(c.ctx, c.rc.Config.StageTimeout(stageNameCI))
	defer cancel()

	result, err := ci.Run(ciCtx, c.rc.GitHub, prURL, c.rc.Config.CI.CheckScope, c.rc.Config.CI.RerunBudget, ciPollInterval)
	if err != nil {
		return err
	}
	if !result.OK {
		if err := c.finish(stageNameCI, stageResultFail, result.Message); err != nil {
			return err
		}
		return c.stageFailure(stageNameCI, result.Message)
	}
	return c.finish(stageNameCI, stageResultPass, result.Message)
}

// parkForApproval records findings and blocks on a single decision per
// stage (findings from the same stage are approved or rejected together,
// not individually) until reviewDecisions resolves it. It is called only
// when a stage's own Result.OK is already true: Review and Document can both
// report success while still carrying findings a human must weigh in on, so
// OK alone is never sufficient to proceed past them.
func (c *chain) parkForApproval(stage string, findings []daemon.AskUserFinding) error {
	if err := c.rm.UpdatePendingFindings(c.runID, findings); err != nil {
		return err
	}
	decision, err := c.reviewDecisions.Wait(c.ctx, c.runID, stage)
	clearErr := c.rm.UpdatePendingFindings(c.runID, nil)
	if err != nil {
		return fmt.Errorf("orchestrator: wait for %s decision: %w", stage, err)
	}
	if clearErr != nil {
		return clearErr
	}
	if decision == daemon.ReviewRejected {
		if err := c.finish(stage, stageResultFail, "rejected by reviewer"); err != nil {
			return err
		}
		return c.stageFailure(stage, "rejected by reviewer")
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
