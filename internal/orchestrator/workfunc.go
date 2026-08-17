package orchestrator

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/douglasjarquin/made/internal/agent"
	"github.com/douglasjarquin/made/internal/daemon"
	execpkg "github.com/douglasjarquin/made/internal/exec"
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

	pushRemoteName = "origin"
)

// Options carries the per-run parameters that come from outside the pushed
// commit/config itself (agent binary resolution); it defaults to the zero
// value in production and only this package's own tests populate it, to
// stand in a fake agent binary in place of a real one. Document rules come
// from the resolved RunContext.Config instead, since Config itself is only
// resolved at Setup time, after a WorkFunc closure is already built.
type Options struct {
	ReviewOptions review.Options
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
	// merging it is a human decision made cannot observe, so the run remains
	// explicitly awaiting merge rather than becoming terminal.
	message := fmt.Sprintf("all stages passed, PR open, awaiting merge: %s", prResult.PRURL)
	return c.rm.Finish(c.runID, daemon.RunAwaitingMerge, message)
}

func (c *chain) start(stage string) error {
	if err := c.rm.SetCurrentStage(c.runID, stage); err != nil {
		return fmt.Errorf("orchestrator: record %s stage start: %w", stage, err)
	}
	if c.emit != nil {
		c.emit(daemon.Event{Kind: daemon.EventStageStarted, Stage: stage})
	}
	return nil
}

func (c *chain) finish(stage, result, message string) error {
	return c.finishWithEvidence(stage, result, message, nil)
}

func (c *chain) finishWithEvidence(stage, result, message string, evidenceRefs []string) error {
	stageResult := daemon.StageResult{Name: stage, Result: result, Message: message, EvidenceRefs: append([]string(nil), evidenceRefs...)}
	if result == stageResultFail {
		stageResult.Error = message
	}
	c.stages = append(c.stages, stageResult)
	if err := c.rm.UpdateStages(c.runID, append([]daemon.StageResult(nil), c.stages...)); err != nil {
		return fmt.Errorf("orchestrator: record %s stage result: %w", stage, err)
	}
	for _, ref := range evidenceRefs {
		if err := c.rm.AddEvidenceRef(c.runID, ref); err != nil {
			return fmt.Errorf("orchestrator: record %s evidence reference: %w", stage, err)
		}
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
	if err := c.start(stageNameIntent); err != nil {
		return err
	}
	result, err := intent.Check(c.rc.Worktree.Path)
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
	if err := c.start(stageNameRebase); err != nil {
		return err
	}
	result, err := rebase.Run(c.rc.Worktree.Path, c.defaultBranch)
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
	if err := c.start(stageNameReview); err != nil {
		return err
	}

	agentKind, err := c.rc.Config.AgentKind()
	if err != nil {
		return fmt.Errorf("orchestrator: resolve agent kind: %w", err)
	}

	result, err := review.Run(c.ctx, c.rc.Worktree.Path, agentKind, c.opts.ReviewOptions)
	if err != nil {
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
	if err := c.start(stageNameTest); err != nil {
		return err
	}
	result, err := test.Run(c.ctx, c.rc.Worktree.Path, c.runID, c.rc.Config.TestCommand(), c.rc.Evidence)
	if err != nil {
		return err
	}
	if !result.OK {
		if err := c.finishWithEvidence(stageNameTest, stageResultFail, result.Message, c.evidenceRefs("stdout.log", "stderr.log")); err != nil {
			return err
		}
		return c.stageFailure(stageNameTest, result.Message)
	}
	return c.finishWithEvidence(stageNameTest, stageResultPass, result.Message, c.evidenceRefs("stdout.log", "stderr.log"))
}

func (c *chain) documentStage() error {
	if err := c.start(stageNameDocument); err != nil {
		return err
	}
	result, err := document.Run(c.rc.Worktree.Path, c.defaultBranch, deriveDocumentRules(c.rc.Config))
	if err != nil {
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
	if err := c.start(stageNameLint); err != nil {
		return err
	}
	result, err := lint.Run(c.ctx, c.rc.Worktree.Path, c.runID, c.rc.Config.LintCommand(), c.rc.Evidence)
	if err != nil {
		return err
	}
	if !result.OK {
		if err := c.finishWithEvidence(stageNameLint, stageResultFail, result.Message, c.evidenceRefs("stdout.log", "stderr.log")); err != nil {
			return err
		}
		return c.stageFailure(stageNameLint, result.Message)
	}
	return c.finishWithEvidence(stageNameLint, stageResultPass, result.Message, c.evidenceRefs("stdout.log", "stderr.log"))
}

func (c *chain) evidenceRefs(files ...string) []string {
	base := deriveEvidenceRef(c.rc.Evidence, c.runID)
	refs := make([]string, len(files))
	for i, file := range files {
		refs[i] = base + "/" + file
	}
	return refs
}

func (c *chain) pushStage() error {
	if err := c.start(stageNamePush); err != nil {
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
	headSHA, err := outputSHA(c.rc.Worktree.Path)
	if err != nil {
		return fmt.Errorf("orchestrator: record pushed output SHA: %w", err)
	}
	if err := c.rm.UpdateSubmissionOutput(c.runID, headSHA); err != nil {
		return fmt.Errorf("orchestrator: record pushed output SHA: %w", err)
	}
	return c.finish(stageNamePush, stageResultPass, result.Message)
}

func outputSHA(worktreePath string) (string, error) {
	result, err := execpkg.Run(context.Background(), execpkg.Command{
		Name: "git",
		Args: []string{"-C", worktreePath, "rev-parse", "HEAD"},
	})
	if err != nil {
		return "", err
	}
	if result.ExitCode != 0 {
		return "", fmt.Errorf("git rev-parse HEAD: %s", strings.TrimSpace(string(result.Stderr)))
	}
	return strings.TrimSpace(string(result.Stdout)), nil
}

func (c *chain) prStage() (pr.Result, error) {
	if err := c.start(stageNamePR); err != nil {
		return pr.Result{}, err
	}

	title, err := derivePRTitle(c.rc.Worktree.Path)
	if err != nil {
		return pr.Result{}, fmt.Errorf("orchestrator: derive PR title: %w", err)
	}

	result, err := pr.Run(c.ctx, c.rc.GitHub, pr.Options{
		Title:       title,
		Base:        c.defaultBranch,
		Head:        c.branch,
		EvidenceRef: deriveEvidenceRef(c.rc.Evidence, c.runID),
	})
	if err != nil {
		return pr.Result{}, err
	}
	if !result.OK {
		if err := c.finish(stageNamePR, stageResultFail, result.Message); err != nil {
			return pr.Result{}, err
		}
		return pr.Result{}, c.stageFailure(stageNamePR, result.Message)
	}
	if err := c.finish(stageNamePR, stageResultPass, result.Message); err != nil {
		return pr.Result{}, err
	}
	return result, nil
}

func (c *chain) ciStage(prURL string) error {
	if err := c.start(stageNameCI); err != nil {
		return err
	}
	if c.rc.Config.NoCI {
		return c.finish(stageNameCI, stageResultPass, "CI disabled by trusted configuration")
	}
	ciCtx, cancel := context.WithTimeout(c.ctx, ciStageTimeout)
	defer cancel()

	result, err := ci.Run(ciCtx, c.rc.GitHub, prURL, c.rc.Config.CI.RerunBudget, ciPollInterval)
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
		return fmt.Errorf("orchestrator: record %s pending findings: %w", stage, err)
	}
	decision, err := c.reviewDecisions.Wait(c.ctx, c.runID, stage)
	if clearErr := c.rm.UpdatePendingFindings(c.runID, nil); clearErr != nil {
		return fmt.Errorf("orchestrator: clear %s pending findings: %w", stage, clearErr)
	}
	if err != nil {
		return fmt.Errorf("orchestrator: wait for %s decision: %w", stage, err)
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
