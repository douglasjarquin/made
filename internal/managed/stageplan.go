package managed

import (
	"context"
	"fmt"

	"github.com/douglasjarquin/made/internal/config"
	"github.com/douglasjarquin/made/internal/pipeline/test"
	"github.com/douglasjarquin/made/internal/planner"
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
	TestExtras   []test.ExtraCommand // stageTest only
	ReviewSource string              // stageReview only: resolved "internal" or "external"
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
// is unchanged for callers that never pass the flag.
func BuildStagePlan(cfg config.Config, changedPaths []string, reviewSource string) (StagePlan, error) {
	var plan StagePlan

	plan.Review = buildReviewPlan(cfg, reviewSource)

	testExtras, err := laneExtraCommands(cfg, changedPaths)
	if err != nil {
		return StagePlan{}, err
	}
	plan.Test = buildTestPlan(cfg, testExtras)

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

func buildTestPlan(cfg config.Config, extras []test.ExtraCommand) StagePlanEntry {
	if explicitlyDisabled(cfg, stageTest) {
		return StagePlanEntry{Stage: stageTest, State: StagePlanDisabled, Reason: "stages.test.enabled is false"}
	}
	cmd := cfg.TestCommand()
	if len(cmd) == 0 && len(extras) == 0 {
		return StagePlanEntry{Stage: stageTest, State: StagePlanNotConfigured, Reason: "no commands.test and no selected validation-lane command"}
	}
	return StagePlanEntry{Stage: stageTest, State: StagePlanRun, Reason: "test command(s) configured", TestExtras: extras}
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
// Test stage's Full commands. With no lanes configured it returns nil,
// exactly preserving pre-lane behavior (commands.test alone).
func laneExtraCommands(cfg config.Config, changedPaths []string) ([]test.ExtraCommand, error) {
	lanes := cfg.Validation.Lanes
	if len(lanes) == 0 {
		return nil, nil
	}
	decisions, err := planner.SelectLanes(lanes, changedPaths)
	if err != nil {
		return nil, fmt.Errorf("managed: select validation lanes: %w", err)
	}
	var extras []test.ExtraCommand
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
			extras = append(extras, test.ExtraCommand{Name: name, Args: cmd})
		}
	}
	return extras, nil
}

// changedPathsForPlan resolves the exact base_sha..input_sha diff paths used
// to select validation lanes. It uses the same read-only git primitive the
// standalone planner package uses, so lane selection agrees between
// daemon-backed and managed validation for an identical diff.
func changedPathsForPlan(ctx context.Context, workspace, baseSHA, inputSHA string) ([]string, error) {
	return planner.ChangedPaths(ctx, workspace, baseSHA, inputSHA)
}
