package managed

import (
	"context"
	"fmt"

	"github.com/douglasjarquin/made/internal/config"
	"github.com/douglasjarquin/made/internal/pipeline/test"
	"github.com/douglasjarquin/made/internal/planner"
	"github.com/douglasjarquin/made/internal/receipt"
)

// StagePlanState is a stage's coverage state before it executes: whether
// trusted policy configures and enables it at all. It is distinct from
// Outcome (a stage's or run's result after execution) even though both are
// surfaced through the same "outcome" field on stage.completed events, so a
// caller sees one vocabulary for "did this stage produce evidence" instead
// of two different fields to reconcile.
type StagePlanState string

const (
	StagePlanRun           StagePlanState = "run"
	StagePlanNotConfigured StagePlanState = "not_configured"
	StagePlanDisabled      StagePlanState = "disabled"
)

// StagePlanEntry is one stage's planned coverage, computed once before any
// stage executes so the terminal result can always identify exactly which
// stages ran and which were absent or disabled - even when a run stops
// early at the first blocking stage.
type StagePlanEntry struct {
	Stage        string
	State        StagePlanState
	Reason       string
	TestExtras   []test.ExtraCommand // stageTest only: lane Full commands that must actually run
	TestReused   []ReusedLaneCommand // stageTest only: lane Full commands satisfied by an existing receipt
	ReviewSource string              // stageReview only: resolved "internal" or "external"
}

// LaneReuseContext carries the exact-SHA identity managed mode has already
// verified before any stage runs: the workspace being validated and the
// pinned base/candidate commits. laneExtraCommands uses it, together with
// trusted config, to build the same Fingerprint the daemon-backed pipeline
// builds (internal/receipt.BuildLaneFingerprint) and consult the read-only
// receipt store - internal/managed never calls receipt.Store.Put itself;
// publishing remains the daemon-backed pipeline's job alone.
type LaneReuseContext struct {
	Workspace    string
	BaseSHA      string
	CandidateSHA string
}

// ReusedLaneCommand records enough about a receipt hit to identify the
// source run and fingerprint, mirroring internal/orchestrator's
// reusedLaneCommand shape for the same project issue #33 requirement.
type ReusedLaneCommand struct {
	Name            string `json:"name"`
	FingerprintHash string `json:"fingerprint_hash"`
	SourceRunID     string `json:"source_run_id"`
}

// laneTestPlan is laneExtraCommands' result: Extras are the lane Full
// commands the Test stage must actually run, Reused are the ones an
// existing receipt already covers.
type laneTestPlan struct {
	Extras []test.ExtraCommand
	Reused []ReusedLaneCommand
}

// StagePlan is the full, policy-derived plan for one managed-validation run.
type StagePlan struct {
	Review   StagePlanEntry
	Test     StagePlanEntry
	Document StagePlanEntry
	Lint     StagePlanEntry
}

func explicitlyDisabled(cfg config.Config, stage string) bool {
	s, ok := cfg.Stages[stage]
	return ok && s.Enabled != nil && !*s.Enabled
}

// reviewEnabledByPolicy reports whether trusted policy wants Review to run
// at all, independent of which ReviewSource will satisfy it: an internal
// agent is configured, or Review is marked required (config.Validate
// already guarantees required implies an agent is configured).
func reviewEnabledByPolicy(cfg config.Config) bool {
	return cfg.Agent != "" || cfg.Review.Required
}

// BuildStagePlan derives each stage's coverage state from trusted policy.
// changedPaths drives validation-lane selection (project issue #33 Phase 2)
// for the Test stage; reviewSource is the caller's --review-source choice,
// defaulting to "internal" when empty so existing internal-agent behavior
// is unchanged for callers that never pass the flag; laneReuse identifies
// the exact run being planned, for the read-only receipt-reuse check
// (project issue #61).
func BuildStagePlan(ctx context.Context, cfg config.Config, changedPaths []string, reviewSource string, laneReuse LaneReuseContext) (StagePlan, error) {
	var plan StagePlan

	plan.Review = buildReviewPlan(cfg, reviewSource)

	testPlan, err := laneExtraCommands(ctx, cfg, changedPaths, laneReuse)
	if err != nil {
		return StagePlan{}, err
	}
	plan.Test = buildTestPlan(cfg, testPlan)

	plan.Document = buildDocumentPlan(cfg)
	plan.Lint = buildLintPlan(cfg)

	return plan, nil
}

func buildReviewPlan(cfg config.Config, reviewSource string) StagePlanEntry {
	if explicitlyDisabled(cfg, stageReview) {
		return StagePlanEntry{Stage: stageReview, State: StagePlanDisabled, Reason: "stages.review.enabled is false"}
	}
	if !reviewEnabledByPolicy(cfg) {
		return StagePlanEntry{Stage: stageReview, State: StagePlanNotConfigured, Reason: "no agent configured and review.required is false"}
	}
	source := reviewSource
	if source == "" {
		source = ReviewSourceInternal
	}
	return StagePlanEntry{Stage: stageReview, State: StagePlanRun, Reason: "policy enables review", ReviewSource: source}
}

func buildTestPlan(cfg config.Config, plan laneTestPlan) StagePlanEntry {
	if explicitlyDisabled(cfg, stageTest) {
		return StagePlanEntry{Stage: stageTest, State: StagePlanDisabled, Reason: "stages.test.enabled is false"}
	}
	cmd := cfg.TestCommand()
	if len(cmd) == 0 && len(plan.Extras) == 0 && len(plan.Reused) == 0 {
		return StagePlanEntry{Stage: stageTest, State: StagePlanNotConfigured, Reason: "no commands.test and no selected validation-lane command"}
	}
	return StagePlanEntry{Stage: stageTest, State: StagePlanRun, Reason: "test command(s) configured", TestExtras: plan.Extras, TestReused: plan.Reused}
}

func buildDocumentPlan(cfg config.Config) StagePlanEntry {
	if explicitlyDisabled(cfg, stageDocument) {
		return StagePlanEntry{Stage: stageDocument, State: StagePlanDisabled, Reason: "stages.document.enabled is false"}
	}
	if len(cfg.Document.Rules) == 0 {
		return StagePlanEntry{Stage: stageDocument, State: StagePlanNotConfigured, Reason: "no document.rules configured"}
	}
	return StagePlanEntry{Stage: stageDocument, State: StagePlanRun, Reason: "document rule(s) configured"}
}

func buildLintPlan(cfg config.Config) StagePlanEntry {
	if explicitlyDisabled(cfg, stageLint) {
		return StagePlanEntry{Stage: stageLint, State: StagePlanDisabled, Reason: "stages.lint.enabled is false"}
	}
	if len(cfg.LintCommand()) == 0 {
		return StagePlanEntry{Stage: stageLint, State: StagePlanNotConfigured, Reason: "no commands.lint configured"}
	}
	return StagePlanEntry{Stage: stageLint, State: StagePlanRun, Reason: "lint command configured"}
}

// laneExtraCommands resolves issue #33's validation.lanes selection for the
// Test stage's Full commands, splitting them into commands that must
// actually run versus commands an existing receipt already covers (project
// issue #61, read-only: this only calls receipt.Store.Get, never Put). With
// no lanes configured it returns a zero-value laneTestPlan, exactly
// preserving pre-lane behavior (commands.test alone).
func laneExtraCommands(ctx context.Context, cfg config.Config, changedPaths []string, reuse LaneReuseContext) (laneTestPlan, error) {
	lanes := cfg.Validation.Lanes
	if len(lanes) == 0 {
		return laneTestPlan{}, nil
	}
	decisions, err := planner.SelectLanes(lanes, changedPaths)
	if err != nil {
		return laneTestPlan{}, fmt.Errorf("managed: select validation lanes: %w", err)
	}

	configHash, err := planner.HashConfig(cfg)
	if err != nil {
		return laneTestPlan{}, fmt.Errorf("managed: hash effective config for validation lanes: %w", err)
	}
	maxAge, err := cfg.Validation.EffectiveReceiptMaxAge()
	if err != nil {
		return laneTestPlan{}, fmt.Errorf("managed: resolve receipt retention window: %w", err)
	}
	repoIdentity := receipt.RepoIdentity(ctx, reuse.Workspace)
	store := &receipt.Store{RepoPath: reuse.Workspace, MaxAge: maxAge}
	repoNoReuse := cfg.Validation.NoReuse

	var plan laneTestPlan
	for _, decision := range decisions {
		if decision.Action != "run" {
			continue
		}
		lane, ok := lanes[decision.Name]
		if !ok {
			continue
		}
		commands := lane.FullShellCommands()
		for i, cmd := range commands {
			name := decision.Name
			if len(commands) > 1 {
				name = fmt.Sprintf("%s-%d", decision.Name, i+1)
			}
			fp := receipt.BuildLaneFingerprint(receipt.LaneFingerprintInputs{
				RepoIdentity: repoIdentity,
				BaseSHA:      reuse.BaseSHA,
				CandidateSHA: reuse.CandidateSHA,
				ConfigHash:   configHash,
				LaneName:     decision.Name,
				MatchedPaths: decision.MatchedPaths,
				Command:      cmd,
				MadeVersion:  MadeVersion,
			})
			if !repoNoReuse && !lane.NoReuse {
				if existing, found, _ := store.Get(ctx, fp.Hash()); found {
					plan.Reused = append(plan.Reused, ReusedLaneCommand{
						Name:            name,
						FingerprintHash: fp.Hash(),
						SourceRunID:     existing.SourceRunID,
					})
					continue
				}
			}
			plan.Extras = append(plan.Extras, test.ExtraCommand{Name: name, Args: cmd})
		}
	}
	return plan, nil
}

// changedPathsForPlan resolves the exact base_sha..input_sha diff paths used
// to select validation lanes. It uses the same read-only git primitive the
// standalone planner package uses, so lane selection agrees between
// daemon-backed and managed validation for an identical diff.
func changedPathsForPlan(ctx context.Context, workspace, baseSHA, inputSHA string) ([]string, error) {
	return planner.ChangedPaths(ctx, workspace, baseSHA, inputSHA)
}
