package managed

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/douglasjarquin/made/internal/agent"
	"github.com/douglasjarquin/made/internal/config"
	"github.com/douglasjarquin/made/internal/evidence"
	"github.com/douglasjarquin/made/internal/pipeline/document"
	"github.com/douglasjarquin/made/internal/pipeline/lint"
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
	plan      StagePlan
	guides    []GuideBinding

	allFindings   []FindingReportedPayload
	stageResults  []StageResult
	evidenceRefs  []string
	decisionsUsed map[string]struct{}

	// reviewAgentResolution records which agent candidate reviewStage used
	// (or, on exhaustion, every candidate it tried and why) whenever
	// Config.Agent is auto/empty; nil when Agent is pinned or the review
	// source is external, since neither path ever probes.
	reviewAgentResolution *agent.AgentResolution
}

// NewRunner constructs a Runner from validated options, parsed config, loaded
// decisions, a stage plan already derived from trusted policy, and the
// trusted base's resolved review guides (project issue #40; nil when none
// are configured).
func NewRunner(opts *Options, cfg config.Config, ew *EventWriter, ev *ManagedEvidenceStore, decisions *Decisions, plan StagePlan, guides []GuideBinding) *Runner {
	return &Runner{
		opts:          opts,
		cfg:           cfg,
		ew:            ew,
		evidence:      ev,
		decisions:     decisions,
		plan:          plan,
		guides:        guides,
		decisionsUsed: make(map[string]struct{}),
	}
}

// Run executes every stage the plan marks StagePlanRun, in order, and emits
// a visible stage.completed record for every not_configured or disabled
// stage without pretending work ran. It stops at the first stage outcome
// that is not passed, not_configured, or disabled. Every planned stage is
// seeded into StageResults before execution starts, so a stage never
// reached because of that early stop stays visible at pending instead of
// being silently omitted.
func (r *Runner) Run(ctx context.Context) (Outcome, string, string) {
	planned := []StagePlanEntry{r.plan.Review, r.plan.Test, r.plan.Document, r.plan.Lint}
	r.stageResults = initialStageResults(planned)
	lastStage := stageLint
	ranAnyStage := false
	for i, entry := range planned {
		lastStage = entry.Stage
		outcome, msg, stoppedAt := r.runStage(ctx, entry, i)
		switch outcome {
		case OutcomePassed, OutcomeNotConfigured, OutcomeDisabled:
			if entry.State == StagePlanRun {
				ranAnyStage = true
			}
			continue
		default:
			return outcome, msg, stoppedAt
		}
	}
	if !ranAnyStage {
		return OutcomeInfrastructureError,
			"policy configures no effective validation work: review, test, document, and lint are all not_configured or disabled",
			lastStage
	}
	return OutcomePassed, "all configured managed validation stages passed", lastStage
}

// initialStageResults seeds one StageResult per planned stage before any
// stage executes: a stage the plan already knows won't run starts at its
// known coverage outcome, and a stage planned to run starts at pending so
// Run's early stop leaves it visibly pending rather than absent.
func initialStageResults(planned []StagePlanEntry) []StageResult {
	results := make([]StageResult, len(planned))
	for i, entry := range planned {
		if entry.State == StagePlanRun {
			results[i] = StageResult{Stage: entry.Stage, Outcome: OutcomePending}
			continue
		}
		results[i] = StageResult{Stage: entry.Stage, Outcome: skipOutcome(entry.State), Message: entry.Reason}
	}
	return results
}

func (r *Runner) runStage(ctx context.Context, entry StagePlanEntry, index int) (Outcome, string, string) {
	stage := entry.Stage
	if err := r.ew.Emit("stage.started", StageStartedPayload{Stage: stage}); err != nil {
		return OutcomeInfrastructureError, fmt.Sprintf("emit stage.started: %s", err), stage
	}

	if entry.State != StagePlanRun {
		msg := entry.Reason
		r.stageResults[index] = StageResult{Stage: stage, Outcome: skipOutcome(entry.State), Message: msg}
		if emitErr := r.ew.Emit("stage.completed", StageCompletedPayload{Stage: stage, Outcome: skipOutcome(entry.State), Message: msg}); emitErr != nil {
			return OutcomeInfrastructureError, fmt.Sprintf("emit stage.completed: %s", emitErr), stage
		}
		return skipOutcome(entry.State), msg, stage
	}

	// Verify HEAD == input_sha and workspace clean before stage.
	if verifyErr := VerifyExactInputSHA(ctx, r.opts.Workspace, r.opts.InputSHA); verifyErr != nil {
		return OutcomeInfrastructureError, fmt.Sprintf("pre-stage validation failed: %s", verifyErr), stage
	}

	// Capture pre-stage worktree state.
	beforeHead, beforeStatus, err := CaptureWorktreeState(ctx, r.opts.Workspace)
	if err != nil {
		return OutcomeInfrastructureError, fmt.Sprintf("capture pre-stage state for %s: %s", stage, err), stage
	}

	outcome, msg, findings, refs := r.executeStage(ctx, entry)

	// Verify workspace unchanged regardless of outcome.
	if mutErr := VerifyWorktreeUnchanged(ctx, r.opts.Workspace, beforeHead, beforeStatus); mutErr != nil {
		_ = r.ew.Emit("stage.completed", StageCompletedPayload{
			Stage:   stage,
			Outcome: OutcomeInfrastructureError,
			Message: "stage mutated workspace: " + mutErr.Error(),
		})
		r.stageResults[index] = StageResult{Stage: stage, Outcome: OutcomeInfrastructureError, Message: mutErr.Error()}
		return OutcomeInfrastructureError, "stage " + stage + " mutated workspace: " + mutErr.Error(), stage
	}

	// Verify HEAD == input_sha and workspace clean after stage.
	if verifyErr := VerifyExactInputSHA(ctx, r.opts.Workspace, r.opts.InputSHA); verifyErr != nil {
		return OutcomeInfrastructureError, fmt.Sprintf("post-stage validation failed: %s", verifyErr), stage
	}

	r.allFindings = append(r.allFindings, findings...)
	r.stageResults[index] = StageResult{Stage: stage, Outcome: outcome, Message: msg, Findings: findings, ReusedCommands: entry.TestReused}

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

func (r *Runner) executeStage(ctx context.Context, entry StagePlanEntry) (Outcome, string, []FindingReportedPayload, []string) {
	switch entry.Stage {
	case stageReview:
		return r.reviewStage(ctx, entry)
	case stageTest:
		return r.testStage(ctx, entry)
	case stageDocument:
		return r.documentStage(ctx)
	case stageLint:
		return r.lintStage(ctx)
	default:
		return OutcomeInfrastructureError, "unknown stage: " + entry.Stage, nil, nil
	}
}

func (r *Runner) reviewStage(ctx context.Context, entry StagePlanEntry) (Outcome, string, []FindingReportedPayload, []string) {
	source, err := resolveReviewSource(entry.ReviewSource)
	if err != nil {
		return OutcomeInfrastructureError, err.Error(), nil, nil
	}

	timeout := r.cfg.StageTimeout(stageReview)
	stageCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	contractHash, hashErr := BuildReviewContract(r.opts.BaseSHA, r.opts.InputSHA, r.opts.PolicyHash, r.guides).Hash()
	if hashErr != nil {
		return OutcomeInfrastructureError, fmt.Sprintf("review: %s", hashErr), nil, nil
	}

	req := ReviewRequest{
		Workspace:    r.opts.Workspace,
		BaseSHA:      r.opts.BaseSHA,
		InputSHA:     r.opts.InputSHA,
		PolicyHash:   r.opts.PolicyHash,
		ContractHash: contractHash,
		Timeout:      timeout,
		RunID:        r.opts.RunID,
		ResultPath:   r.opts.ReviewResult,
		Guides:       r.guides,
	}
	var result ReviewResult
	if entry.ReviewSource == ReviewSourceInternal {
		req.AgentBinaryPath = r.opts.ReviewAgentBinaryPath
		req.AgentExtraEnv = r.opts.ReviewAgentExtraEnv
		spawned, agentErr := r.spawnInternalReview(stageCtx, source, req)
		if agentErr != nil {
			return OutcomeInfrastructureError, fmt.Sprintf("review: %s", agentErr), nil, nil
		}
		if spawned == nil {
			return OutcomeInfrastructureError, formatAgentResolutionFailure(*r.reviewAgentResolution), nil, nil
		}
		result = *spawned
	} else {
		var err error
		result, err = source.Review(stageCtx, req)
		if err != nil {
			return OutcomeInfrastructureError, fmt.Sprintf("review: %s", err), nil, nil
		}
	}

	evidenceData, marshalErr := json.Marshal(result.Findings)
	if marshalErr != nil {
		return OutcomeInfrastructureError, fmt.Sprintf("review: marshal findings: %s", marshalErr), nil, nil
	}
	evidenceFiles := map[string][]byte{"findings.json": evidenceData}
	if len(r.guides) > 0 {
		guideEvidence, guideMarshalErr := json.Marshal(GuideEvidence{
			Configured: r.guides,
			Consulted:  result.GuidesConsulted,
		})
		if guideMarshalErr != nil {
			return OutcomeInfrastructureError, fmt.Sprintf("review: marshal guide evidence: %s", guideMarshalErr), nil, nil
		}
		evidenceFiles["guides.json"] = guideEvidence
	}
	refs, evErr := r.evidence.WriteStageFiles(stageReview, evidenceFiles)
	if evErr != nil {
		return OutcomeInfrastructureError, fmt.Sprintf("review: write evidence: %s", evErr), nil, nil
	}

	// Classify findings.
	var findings []FindingReportedPayload
	outcome := OutcomePassed
	var msg string

	for _, f := range result.Findings {
		// Validate managed findings have sufficient structural identity.
		if validationErr := ValidateStableFindingIdentity(FingerprintInput{
			Stage:           stageReview,
			Kind:            string(f.Kind),
			Code:            f.Code,
			Class:           f.Class,
			Symbol:          f.Symbol,
			Paths:           f.Paths,
			Description:     f.Description,
			WorkspacePrefix: r.opts.Workspace,
		}); validationErr != nil {
			return OutcomeInfrastructureError, fmt.Sprintf("review: invalid finding identity: %s", validationErr), nil, nil
		}

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
			Description: evidence.RedactString(f.Description),
			Paths:       f.Paths,
			Symbol:      f.Symbol,
			Patch:       f.Patch,
		}
		findings = append(findings, payload)
		if emitErr := r.ew.Emit("finding.reported", payload); emitErr != nil {
			return OutcomeInfrastructureError, fmt.Sprintf("emit finding.reported: %s", emitErr), nil, nil
		}

		switch f.Kind {
		case agent.FindingBlocking:
			outcome = mergeOutcome(outcome, OutcomeFailedTerminal)
			msg = fmt.Sprintf("blocking finding: %s", evidence.RedactString(f.Description))
		case agent.FindingAutoFixable:
			outcome = mergeOutcome(outcome, OutcomeFailedRetryable)
			if msg == "" {
				msg = "auto-fixable finding(s) require repair"
			}
		case agent.FindingAskUser:
			dec, hasDec := r.decisions.Lookup(fp)
			if !hasDec {
				outcome = mergeOutcome(outcome, OutcomeNeedsDecision)
				if msg == "" {
					msg = "ask-user finding(s) require a Decision"
				}
			} else {
				r.decisionsUsed[fp] = struct{}{}
				if dec.Outcome == DecisionRejected {
					outcome = mergeOutcome(outcome, OutcomeFailedTerminal)
					msg = fmt.Sprintf("ask-user finding rejected by decision %s: %s", dec.DecisionID, evidence.RedactString(f.Description))
				}
				// approved: continue (outcome stays passed or existing failure)
			}
		}
	}

	if !result.OK {
		outcome = mergeOutcome(outcome, OutcomeFailedTerminal)
		if msg == "" {
			msg = result.Message
		}
	}
	if msg == "" {
		msg = result.Message
	}

	// Check for duplicate fingerprints in this stage (blocker 1: prevent ambiguous decisions).
	if dupErr := r.checkDuplicateFingerprints(stageReview, findings); dupErr != nil {
		return OutcomeInfrastructureError, dupErr.Error(), nil, refs
	}

	return outcome, msg, findings, refs
}

// spawnInternalReview resolves which agent kind to review with and calls
// source.Review. An explicit non-auto Agent (AgentIsPinned) always skips
// probing entirely and behaves exactly as before agent auto-resolve;
// otherwise it probes the candidate list (agent.Resolve) and, on a
// classified capacity failure, retries with the remaining candidates
// after the one that just failed - any other error is a hard failure,
// exactly as today. r.reviewAgentResolution always records the outcome
// (nil only on the pinned path). A (nil, nil) return means every
// candidate was exhausted; r.reviewAgentResolution carries the full
// structured reason for the caller to surface.
func (r *Runner) spawnInternalReview(ctx context.Context, source ReviewSource, req ReviewRequest) (*ReviewResult, error) {
	if r.cfg.AgentIsPinned() {
		kind, err := r.cfg.AgentKind()
		if err != nil {
			return nil, fmt.Errorf("resolve agent kind: %w", err)
		}
		req.AgentKind = kind
		result, err := source.Review(ctx, req)
		if err != nil {
			return nil, err
		}
		return &result, nil
	}

	remaining := r.cfg.AgentCandidates()
	var attempts []agent.CandidateAttempt
	for {
		res := agent.Resolve(ctx, remaining)
		attempts = append(attempts, res.Attempts...)
		if res.AllExhausted() {
			r.reviewAgentResolution = &agent.AgentResolution{Attempts: attempts}
			return nil, nil
		}
		req.AgentKind = *res.Selected
		result, err := source.Review(ctx, req)
		if err == nil {
			r.reviewAgentResolution = &agent.AgentResolution{Selected: res.Selected, Attempts: attempts}
			return &result, nil
		}
		if !errors.Is(err, agent.ErrAgentCapacity) {
			return nil, err
		}
		attempts = append(attempts, agent.CandidateAttempt{Kind: *res.Selected, Reason: agent.ReasonQuotaExhausted})
		remaining = remainingAfterKind(remaining, *res.Selected)
	}
}

// remainingAfterKind returns the candidates after the given kind's position
// in the list, so a capacity retry only ever moves forward through the
// resolved order and never revisits an already-failed candidate.
func remainingAfterKind(candidates []agent.Kind, after agent.Kind) []agent.Kind {
	for i, kind := range candidates {
		if kind == after {
			return candidates[i+1:]
		}
	}
	return nil
}

// formatAgentResolutionFailure renders the brief's required escalation
// shape ("missing/unauthenticated/quota-exhausted-until-<resetsAt>") as the
// stage-failure message; structured JSON surfacing lives on
// r.reviewAgentResolution for a later stage-result field to expose.
func formatAgentResolutionFailure(res agent.AgentResolution) string {
	parts := make([]string, 0, len(res.Attempts))
	for _, a := range res.Attempts {
		reason := string(a.Reason)
		if a.Reason == agent.ReasonQuotaExhausted && a.QuotaResetsAt != nil {
			reason = fmt.Sprintf("quota-exhausted-until-%s", a.QuotaResetsAt.Format(time.RFC3339))
		}
		parts = append(parts, fmt.Sprintf("%s (%s)", a.Kind, reason))
	}
	return "no review agent available: " + strings.Join(parts, ", ")
}

func (r *Runner) testStage(ctx context.Context, entry StagePlanEntry) (Outcome, string, []FindingReportedPayload, []string) {
	cmd := r.cfg.TestCommand()
	if len(cmd) == 0 && len(entry.TestExtras) == 0 {
		return OutcomePassed, fmt.Sprintf("%d command(s) reused, 0 executed", len(entry.TestReused)), nil, nil
	}

	timeout := r.cfg.StageTimeout(stageTest)
	stageCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ev := &stageEvidenceShim{store: r.evidence, stage: stageTest}
	result, err := test.Run(stageCtx, r.opts.Workspace, r.opts.RunID, cmd, entry.TestExtras, ev)
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

	// Use exact SHA range through safegit.
	result, err := document.RunContextWithRange(stageCtx, r.opts.Workspace, r.opts.BaseSHA, r.opts.InputSHA, rules)
	if err != nil {
		return OutcomeInfrastructureError, fmt.Sprintf("document: %s", err), nil, nil
	}

	var findings []FindingReportedPayload
	outcome := OutcomePassed
	msg := result.Message

	for _, f := range result.Findings {
		// Validate managed findings have sufficient structural identity.
		if validationErr := ValidateStableFindingIdentity(FingerprintInput{
			Stage:           stageDocument,
			Kind:            string(f.Kind),
			Code:            f.Code,
			Class:           f.Class,
			Symbol:          f.Symbol,
			Paths:           f.Paths,
			Description:     f.Description,
			WorkspacePrefix: r.opts.Workspace,
		}); validationErr != nil {
			return OutcomeInfrastructureError, fmt.Sprintf("document: invalid finding identity: %s", validationErr), nil, nil
		}

		fp := Fingerprint(FingerprintInput{
			Stage:           stageDocument,
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
			Stage:       stageDocument,
			Kind:        string(f.Kind),
			Code:        f.Code,
			Class:       f.Class,
			Description: evidence.RedactString(f.Description),
			Paths:       f.Paths,
			Symbol:      f.Symbol,
		}
		findings = append(findings, payload)
		if emitErr := r.ew.Emit("finding.reported", payload); emitErr != nil {
			return OutcomeInfrastructureError, fmt.Sprintf("emit finding.reported: %s", emitErr), nil, nil
		}

		dec, hasDec := r.decisions.Lookup(fp)
		if !hasDec {
			outcome = mergeOutcome(outcome, OutcomeNeedsDecision)
			if msg == "" || outcome == OutcomeNeedsDecision {
				msg = "document finding(s) require a Decision"
			}
		} else {
			r.decisionsUsed[fp] = struct{}{}
			if dec.Outcome == DecisionRejected {
				outcome = mergeOutcome(outcome, OutcomeFailedTerminal)
				msg = fmt.Sprintf("document finding rejected by decision %s: %s", dec.DecisionID, evidence.RedactString(f.Description))
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

	// Check for duplicate fingerprints in this stage (blocker 1: prevent ambiguous decisions).
	if dupErr := r.checkDuplicateFingerprints(stageDocument, findings); dupErr != nil {
		return OutcomeInfrastructureError, dupErr.Error(), nil, refs
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

// checkDuplicateFingerprints verifies that no two findings in a stage have the same fingerprint.
// This prevents ambiguous decision application where a Decision might approve or reject the
// wrong finding. Returns an error if duplicates are found.
func (r *Runner) checkDuplicateFingerprints(stage string, findings []FindingReportedPayload) error {
	seen := make(map[string]int) // fingerprint -> count
	for _, f := range findings {
		seen[f.Fingerprint]++
	}
	for fp, count := range seen {
		if count > 1 {
			return fmt.Errorf("stage %s: %d findings share fingerprint %s; cannot apply decisions unambiguously", stage, count, fp)
		}
	}
	return nil
}

// DecisionsApplied returns the fingerprints of decisions that matched a finding.
func (r *Runner) DecisionsApplied() []string {
	applied := make([]string, 0, len(r.decisionsUsed))
	for fp := range r.decisionsUsed {
		applied = append(applied, fp)
	}
	return applied
}

func skipOutcome(state StagePlanState) Outcome {
	if state == StagePlanDisabled {
		return OutcomeDisabled
	}
	return OutcomeNotConfigured
}

// outcomeRank ranks outcomes so the most severe outcome wins when merging.
func outcomeRank(o Outcome) int {
	switch o {
	case OutcomePassed:
		return 0
	case OutcomeNeedsDecision:
		return 1
	case OutcomeFailedRetryable:
		return 2
	case OutcomeFailedTerminal, OutcomeInfrastructureError:
		return 3
	default:
		return 1
	}
}

// mergeOutcome returns the more severe of two outcomes.
func mergeOutcome(current, next Outcome) Outcome {
	if outcomeRank(next) > outcomeRank(current) {
		return next
	}
	return current
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
