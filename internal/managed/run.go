package managed

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
)

// MadeVersion is embedded in terminal evidence. Override with ldflags during release builds.
var MadeVersion = "dev"

// Run executes the full managed-validation lifecycle.
// stdout is dedicated to the JSON-Lines event stream; diagnostics go to stderr.
//
// Exit codes:
//   - 0  passed
//   - 1  infrastructure_error
//   - 2  usage/contract error (no JSON stream emitted)
//   - 3  needs_decision
//   - 4  failed_retryable
//   - 5  failed_terminal
//   - 130 canceled
func Run(ctx context.Context, opts *Options, stdout, stderr *os.File) int {
	// Phase 0: Validate argument formats (usage errors → exit 2, no events).
	if err := ValidateOptions(opts); err != nil {
		_, _ = fmt.Fprintln(stderr, "made validate:", err)
		return 2
	}

	// Generate a per-invocation ID to prevent evidence path collisions across reruns.
	invID, err := generateInvocationID()
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "made validate: generate invocation ID:", err)
		return 2
	}
	opts.InvocationID = invID

	ew := NewEventWriter(stdout, opts)

	// Emit run.started before any infra work.
	// All subsequent failures must emit run.completed before returning.
	if err := ew.Emit("run.started", RunStartedPayload{}); err != nil {
		_, _ = fmt.Fprintln(stderr, "made validate: emit run.started:", err)
		return 1
	}

	// emitInfraError emits a terminal infrastructure_error event, logs to stderr,
	// and returns the appropriate exit code (1).
	emitInfraError := func(msg string) int {
		_, _ = fmt.Fprintln(stderr, "made validate:", msg)
		_ = ew.EmitTerminal(RunCompletedPayload{
			Outcome:      OutcomeInfrastructureError,
			Message:      msg,
			InvocationID: invID,
		})
		return OutcomeInfrastructureError.ExitCode()
	}

	// Phase 1: Infrastructure preflight.
	preflightResult, err := RunPreflight(ctx, opts)
	if err != nil {
		if ctx.Err() != nil {
			_ = ew.EmitTerminal(RunCompletedPayload{
				Outcome:      OutcomeCanceled,
				Message:      "canceled during preflight: " + ctx.Err().Error(),
				InvocationID: invID,
			})
			return OutcomeCanceled.ExitCode()
		}
		return emitInfraError("preflight: " + err.Error())
	}

	// Phase 2: Parse config from verified bytes (never re-read the file).
	cfg, err := loadConfig(preflightResult.ConfigBytes)
	if err != nil {
		return emitInfraError("parse trusted config: " + err.Error())
	}

	// Phase 2b: Resolve and hash every configured guide from the trusted
	// base (project issue #40), before Review or any other stage runs.
	guides, err := ResolveTrustedGuides(TrustedGuideRoot(opts.TrustedConfig), cfg.Review.Guides)
	if err != nil {
		return emitInfraError("resolve review guides: " + err.Error())
	}

	// Phase 3: Load decisions (optional).
	decs, err := readDecisions(opts.DecisionsPath, opts)
	if err != nil {
		return emitInfraError("load decisions: " + err.Error())
	}

	// Phase 4: Create evidence store.
	ev := NewManagedEvidenceStore(opts.EvidenceDir, opts.RunID, invID)
	if mkErr := os.MkdirAll(ev.InvocationDir(), 0o750); mkErr != nil {
		return emitInfraError("create evidence directory: " + mkErr.Error())
	}

	// Phase 5: Build the policy-derived stage plan. No stage runs that
	// trusted policy does not configure and enable.
	changedPaths, err := changedPathsForPlan(ctx, opts.Workspace, opts.BaseSHA, opts.InputSHA)
	if err != nil {
		return emitInfraError("compute changed paths for validation lanes: " + err.Error())
	}
	plan, err := BuildStagePlan(cfg, changedPaths, opts.ReviewSource)
	if err != nil {
		return emitInfraError("build stage plan: " + err.Error())
	}

	// Phase 6: Run validation stages. Runner.Run reports infrastructure_error
	// itself when the plan has no effective work, after emitting a visible
	// not_configured/disabled record for every stage - never silently.

	runner := NewRunner(opts, cfg, ew, ev, decs, plan, guides)
	outcome, msg, stoppedAt := runner.Run(ctx)

	if ctx.Err() != nil {
		outcome = OutcomeCanceled
		msg = "canceled: " + ctx.Err().Error()
		stoppedAt = "canceled"
	}

	// Phase 6: Write terminal evidence.
	// This is the single authoritative summary of the run outcome.
	// Any failure here overrides the outcome to infrastructure_error.
	manifest := buildTerminalManifest(opts, runner, invID, outcome, ew.Sequence()+1)
	if writeErr := ev.WriteTerminal(manifest); writeErr != nil {
		outcome = OutcomeInfrastructureError
		msg = "terminal evidence write failed: " + writeErr.Error()
		_, _ = fmt.Fprintln(stderr, "made validate:", msg)
	}

	// Report unused decisions.
	for _, unused := range runner.UnusedDecisions() {
		_, _ = fmt.Fprintf(stderr, "made validate: unused decision %s (fingerprint %s)\n",
			unused.DecisionID, unused.FindingFingerprint)
	}

	// Phase 7: Emit terminal run.completed event.
	if err := ew.EmitTerminal(RunCompletedPayload{
		Outcome:      outcome,
		Stage:        stoppedAt,
		Message:      msg,
		InvocationID: invID,
		Findings:     runner.AllFindings(),
		EvidenceRefs: runner.EvidenceRefs(),
	}); err != nil {
		_, _ = fmt.Fprintln(stderr, "made validate: emit run.completed:", err)
		return 1
	}

	return outcome.ExitCode()
}

// generateInvocationID returns a random 16-character lowercase hex string.
func generateInvocationID() (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

// buildTerminalManifest constructs the run terminal evidence summary.
func buildTerminalManifest(opts *Options, runner *Runner, invID string, outcome Outcome, eventCount int) *TerminalManifest {
	return &TerminalManifest{
		RunID:            opts.RunID,
		MissionID:        opts.MissionID,
		InvocationID:     invID,
		BaseSHA:          opts.BaseSHA,
		InputSHA:         opts.InputSHA,
		PolicyHash:       opts.PolicyHash,
		StageResults:     runner.StageResults(),
		Findings:         runner.AllFindings(),
		DecisionsApplied: runner.DecisionsApplied(),
		Outcome:          outcome,
		EventCount:       eventCount,
		EvidenceRefs:     runner.EvidenceRefs(),
		MadeVersion:      MadeVersion,
	}
}
