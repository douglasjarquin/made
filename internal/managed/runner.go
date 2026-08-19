package managed

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/douglasjarquin/made/internal/agent"
	"github.com/douglasjarquin/made/internal/config"
	"github.com/douglasjarquin/made/internal/pipeline/document"
	"github.com/douglasjarquin/made/internal/pipeline/lint"
	"github.com/douglasjarquin/made/internal/pipeline/review"
	"github.com/douglasjarquin/made/internal/pipeline/test"
)

const (
	stageReview   = "review"
	stageTest     = "test"
	stageDocument = "document"
	stageLint     = "lint"
)

// Runner executes managed validation stages.
type Runner struct {
	opts      *Options
	cfg       config.Config
	ew        *EventWriter
	evidence  *ManagedEvidenceStore
	decisions *Decisions

	allFindings   []FindingReportedPayload
	stageResults  []StageResult
	evidenceRefs  []string
	decisionsUsed map[string]struct{}
}

// NewRunner constructs a Runner from validated options, parsed config, and loaded decisions.
func NewRunner(opts *Options, cfg config.Config, ew *EventWriter, ev *ManagedEvidenceStore, decisions *Decisions) *Runner {
	return &Runner{
		opts:          opts,
		cfg:           cfg,
		ew:            ew,
		evidence:      ev,
		decisions:     decisions,
		decisionsUsed: make(map[string]struct{}),
	}
}

// Run executes all managed validation stages in order.
// It returns the terminal outcome. The caller is responsible for emitting the
// terminal event via ew.EmitTerminal.
func (r *Runner) Run(ctx context.Context) (Outcome, string, string) {
	stages := []string{stageReview, stageTest, stageDocument, stageLint}
	for _, stage := range stages {
		outcome, msg, stoppedAt := r.runStage(ctx, stage)
		if outcome != OutcomePassed {
			return outcome, msg, stoppedAt
		}
	}
	return OutcomePassed, "all managed validation stages passed", stageLint
}

func (r *Runner) runStage(ctx context.Context, stage string) (Outcome, string, string) {
	if err := r.ew.Emit("stage.started", StageStartedPayload{Stage: stage}); err != nil {
		return OutcomeInfrastructureError, fmt.Sprintf("emit stage.started: %s", err), stage
	}

	// Capture pre-stage worktree state.
	beforeHead, beforeStatus, err := CaptureWorktreeState(ctx, r.opts.Workspace)
	if err != nil {
		return OutcomeInfrastructureError, fmt.Sprintf("capture pre-stage state for %s: %s", stage, err), stage
	}

	outcome, msg, findings, refs := r.executeStage(ctx, stage)

	// Verify workspace unchanged regardless of outcome.
	if mutErr := VerifyWorktreeUnchanged(ctx, r.opts.Workspace, beforeHead, beforeStatus); mutErr != nil {
		_ = r.ew.Emit("stage.completed", StageCompletedPayload{
			Stage:   stage,
			Outcome: OutcomeInfrastructureError,
			Message: "stage mutated workspace: " + mutErr.Error(),
		})
		r.stageResults = append(r.stageResults, StageResult{Stage: stage, Outcome: OutcomeInfrastructureError, Message: mutErr.Error()})
		return OutcomeInfrastructureError, "stage " + stage + " mutated workspace: " + mutErr.Error(), stage
	}

	r.allFindings = append(r.allFindings, findings...)
	r.stageResults = append(r.stageResults, StageResult{Stage: stage, Outcome: outcome, Message: msg, Findings: findings})

	for _, ref := range refs {
		r.evidenceRefs = append(r.evidenceRefs, ref)
		if emitErr := r.ew.Emit("evidence.created", EvidenceCreatedPayload{Stage: stage, Path: ref}); emitErr != nil {
			return OutcomeInfrastructureError, fmt.Sprintf("emit evidence.created: %s", emitErr), stage
		}
	}

	if emitErr := r.ew.Emit("stage.completed", StageCompletedPayload{Stage: stage, Outcome: outcome, Message: msg}); emitErr != nil {
		return OutcomeInfrastructureError, fmt.Sprintf("emit stage.completed: %s", emitErr), stage
	}

	return outcome, msg, stage
}

func (r *Runner) executeStage(ctx context.Context, stage string) (Outcome, string, []FindingReportedPayload, []string) {
	switch stage {
	case stageReview:
		return r.reviewStage(ctx)
	case stageTest:
		return r.testStage(ctx)
	case stageDocument:
		return r.documentStage(ctx)
	case stageLint:
		return r.lintStage(ctx)
	default:
		return OutcomeInfrastructureError, "unknown stage: " + stage, nil, nil
	}
}

func (r *Runner) reviewStage(ctx context.Context) (Outcome, string, []FindingReportedPayload, []string) {
	agentKind, err := r.cfg.AgentKind()
	if err != nil {
		return OutcomeInfrastructureError, fmt.Sprintf("resolve agent kind: %s", err), nil, nil
	}

	timeout := r.cfg.StageTimeout(stageReview)
	stageCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	reviewOpts := review.Options{
		BaseBranch:    r.opts.BaseSHA, // exact SHA used as base ref
		ReportOnly:    true,           // managed mode: never apply auto-fixes
		EvidenceRunID: r.opts.RunID,
	}

	result, err := review.Run(stageCtx, r.opts.Workspace, agentKind, reviewOpts)
	if err != nil {
		return OutcomeInfrastructureError, fmt.Sprintf("review: %s", err), nil, nil
	}

	// Write evidence.
	evidenceData, marshalErr := json.Marshal(result.Findings)
	if marshalErr != nil {
		return OutcomeInfrastructureError, fmt.Sprintf("review: marshal findings: %s", marshalErr), nil, nil
	}
	refs, evErr := r.evidence.WriteStageFiles(stageReview, map[string][]byte{
		"findings.json": evidenceData,
	})
	if evErr != nil {
		return OutcomeInfrastructureError, fmt.Sprintf("review: write evidence: %s", evErr), nil, nil
	}

	// Classify findings.
	var findings []FindingReportedPayload
	outcome := OutcomePassed
	var msg string

	for _, f := range result.Findings {
		fp := Fingerprint(FingerprintInput{
			Stage:           stageReview,
			Kind:            string(f.Kind),
			Code:            f.Code,
			Class:           f.Class,
			Symbol:          f.Symbol,
			Paths:           f.Paths,
			Description:     f.Description,
			WorkspacePrefix: r.opts.Workspace,
		})
		payload := FindingReportedPayload{
			Fingerprint: fp,
			Stage:       stageReview,
			Kind:        string(f.Kind),
			Code:        f.Code,
			Class:       f.Class,
			Description: f.Description,
			Paths:       f.Paths,
			Symbol:      f.Symbol,
			Patch:       f.Patch,
		}
		findings = append(findings, payload)
		_ = r.ew.Emit("finding.reported", payload)

		switch f.Kind {
		case agent.FindingBlocking:
			outcome = OutcomeFailedTerminal
			msg = fmt.Sprintf("blocking finding: %s", f.Description)
		case agent.FindingAutoFixable:
			if outcome == OutcomePassed {
				outcome = OutcomeFailedRetryable
				msg = "auto-fixable finding(s) require repair"
			}
		case agent.FindingAskUser:
			dec, hasDec := r.decisions.Lookup(fp)
			if !hasDec {
				if outcome == OutcomePassed || outcome == OutcomeFailedRetryable {
					outcome = OutcomeNeedsDecision
					msg = "ask-user finding(s) require a Decision"
				}
			} else {
				r.decisionsUsed[fp] = struct{}{}
				if dec.Outcome == DecisionRejected {
					outcome = OutcomeFailedTerminal
					msg = fmt.Sprintf("ask-user finding rejected by decision %s: %s", dec.DecisionID, f.Description)
				}
				// approved: continue (outcome stays passed or existing failure)
			}
		}
	}

	if !result.OK && outcome == OutcomePassed {
		outcome = OutcomeFailedTerminal
		msg = result.Message
	}
	if msg == "" {
		msg = result.Message
	}

	return outcome, msg, findings, refs
}

func (r *Runner) testStage(ctx context.Context) (Outcome, string, []FindingReportedPayload, []string) {
	cmd := r.cfg.TestCommand()
	if len(cmd) == 0 {
		return OutcomeInfrastructureError, "no test command configured", nil, nil
	}

	timeout := r.cfg.StageTimeout(stageTest)
	stageCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Use a simple evidence store shim.
	ev := &stageEvidenceShim{store: r.evidence, stage: stageTest}
	result, err := test.Run(stageCtx, r.opts.Workspace, r.opts.RunID, cmd, ev)
	if err != nil {
		return OutcomeInfrastructureError, fmt.Sprintf("test: %s", err), nil, nil
	}

	refs := ev.refs
	if !result.OK {
		return OutcomeFailedRetryable, result.Message, nil, refs
	}
	return OutcomePassed, result.Message, nil, refs
}

func (r *Runner) documentStage(ctx context.Context) (Outcome, string, []FindingReportedPayload, []string) {
	rules := deriveDocumentRules(r.cfg)

	timeout := r.cfg.StageTimeout(stageDocument)
	stageCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Use exact SHA range.
	result, err := document.RunContextWithBaseSHA(stageCtx, r.opts.Workspace, r.opts.BaseSHA, rules)
	if err != nil {
		return OutcomeInfrastructureError, fmt.Sprintf("document: %s", err), nil, nil
	}

	var findings []FindingReportedPayload
	outcome := OutcomePassed
	msg := result.Message

	for _, f := range result.Findings {
		fp := Fingerprint(FingerprintInput{
			Stage:           stageDocument,
			Kind:            string(f.Kind),
			Paths:           f.Paths,
			Description:     f.Description,
			WorkspacePrefix: r.opts.Workspace,
		})
		payload := FindingReportedPayload{
			Fingerprint: fp,
			Stage:       stageDocument,
			Kind:        string(f.Kind),
			Description: f.Description,
			Paths:       f.Paths,
		}
		findings = append(findings, payload)
		_ = r.ew.Emit("finding.reported", payload)

		dec, hasDec := r.decisions.Lookup(fp)
		if !hasDec {
			if outcome == OutcomePassed {
				outcome = OutcomeNeedsDecision
				msg = "document finding(s) require a Decision"
			}
		} else {
			r.decisionsUsed[fp] = struct{}{}
			if dec.Outcome == DecisionRejected {
				outcome = OutcomeFailedTerminal
				msg = fmt.Sprintf("document finding rejected by decision %s: %s", dec.DecisionID, f.Description)
			}
		}
	}

	// Write evidence.
	evidenceData, marshalErr := json.Marshal(result.Findings)
	if marshalErr != nil {
		return OutcomeInfrastructureError, fmt.Sprintf("document: marshal findings: %s", marshalErr), findings, nil
	}
	refs, evErr := r.evidence.WriteStageFiles(stageDocument, map[string][]byte{
		"findings.json": evidenceData,
	})
	if evErr != nil {
		return OutcomeInfrastructureError, fmt.Sprintf("document: write evidence: %s", evErr), findings, nil
	}

	return outcome, msg, findings, refs
}

func (r *Runner) lintStage(ctx context.Context) (Outcome, string, []FindingReportedPayload, []string) {
	cmd := r.cfg.LintCommand()

	timeout := r.cfg.StageTimeout(stageLint)
	stageCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ev := &stageEvidenceShim{store: r.evidence, stage: stageLint}
	result, err := lint.Run(stageCtx, r.opts.Workspace, r.opts.RunID, cmd, ev)
	if err != nil {
		return OutcomeInfrastructureError, fmt.Sprintf("lint: %s", err), nil, nil
	}

	refs := ev.refs
	if !result.OK {
		return OutcomeFailedRetryable, result.Message, nil, refs
	}
	return OutcomePassed, result.Message, nil, refs
}

func (r *Runner) AllFindings() []FindingReportedPayload {
	return r.allFindings
}

func (r *Runner) EvidenceRefs() []string {
	return r.evidenceRefs
}

func (r *Runner) StageResults() []StageResult {
	return r.stageResults
}

func (r *Runner) UnusedDecisions() []DecisionRecord {
	var unused []DecisionRecord
	for _, d := range r.decisions.All() {
		if _, used := r.decisionsUsed[d.FindingFingerprint]; !used {
			unused = append(unused, d)
		}
	}
	return unused
}

func deriveDocumentRules(cfg config.Config) []document.Rule {
	rules := make([]document.Rule, 0, len(cfg.Document.Rules))
	for _, r := range cfg.Document.Rules {
		rules = append(rules, document.Rule{SourcePattern: r.PathPattern, DocPattern: r.RequiredDocPattern})
	}
	return rules
}

// stageEvidenceShim adapts ManagedEvidenceStore to the evidence.Store interface
// used by test and lint stages.
type stageEvidenceShim struct {
	store *ManagedEvidenceStore
	stage string
	refs  []string
}

func (s *stageEvidenceShim) WriteEvidence(runID string, files map[string][]byte) error {
	refs, err := s.store.WriteStageFiles(s.stage, files)
	if err != nil {
		return err
	}
	s.refs = append(s.refs, refs...)
	return nil
}

func (s *stageEvidenceShim) WriteEvidenceContext(ctx context.Context, runID string, files map[string][]byte) error {
	return s.WriteEvidence(runID, files)
}

// loadConfig parses verified config bytes into a config.Config.
// The bytes must already have been hash-verified by preflight.
func loadConfig(configBytes []byte) (config.Config, error) {
	cfg, err := config.ParseBytes(configBytes)
	if err != nil {
		return config.Config{}, fmt.Errorf("managed: parse trusted config: %w", err)
	}
	return cfg, nil
}

// readDecisions loads and validates the optional Decisions file.
func readDecisions(path string, opts *Options) (*Decisions, error) {
	if path == "" {
		return &Decisions{byFingerprint: make(map[string]DecisionRecord)}, nil
	}
	return LoadDecisions(path, opts)
}

// buildTerminalManifest constructs the run terminal evidence summary.
func buildTerminalManifest(opts *Options, runner *Runner, outcome Outcome, eventCount int) TerminalManifest {
	var decisionsApplied []string
	for fp := range runner.decisionsUsed {
		decisionsApplied = append(decisionsApplied, fp)
	}
	return TerminalManifest{
		RunID:            opts.RunID,
		MissionID:        opts.MissionID,
		BaseSHA:          opts.BaseSHA,
		InputSHA:         opts.InputSHA,
		PolicyHash:       opts.PolicyHash,
		StageResults:     runner.StageResults(),
		Findings:         runner.AllFindings(),
		DecisionsApplied: decisionsApplied,
		Outcome:          outcome,
		EventCount:       eventCount,
		EvidenceRefs:     runner.EvidenceRefs(),
	}
}

// isContextError checks if an error is a context cancellation or deadline.
func isContextError(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "context canceled") || strings.Contains(s, "context deadline exceeded")
}
