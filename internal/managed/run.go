package managed

import (
	"context"
	"fmt"
	"os"
)

// Run executes the full managed-validation lifecycle:
//  1. Preflight verification
//  2. Config loading and validation
//  3. Decisions loading
//  4. run.started event
//  5. Stage execution (review → test → document → lint)
//  6. Evidence terminal manifest
//  7. run.completed terminal event
//
// stdout must be dedicated to the JSON-Lines event stream.
// Diagnostics are written to stderr.
func Run(ctx context.Context, opts *Options, stdout, stderr *os.File) int {
	ew := NewEventWriter(stdout, opts)

	// Preflight: verify all preconditions before emitting any events.
	preflightResult, err := RunPreflight(ctx, opts)
	if err != nil {
		if isContextError(err) {
			_ = ew.EmitTerminal(RunCompletedPayload{
				Outcome: OutcomeCanceled,
				Message: "canceled during preflight: " + err.Error(),
			})
			return OutcomeCanceled.ExitCode()
		}
		_, _ = fmt.Fprintln(stderr, "made validate: preflight failed:", err)
		// Preflight failures before the first event: no terminal JSON emitted.
		// Exit 2 for contract/usage errors (wrong SHA, bad hash format, etc.),
		// exit 1 for infrastructure errors (file not found, Git failure).
		return 1
	}

	// Parse the verified config bytes (never re-read the file).
	cfg, err := loadConfig(preflightResult.ConfigBytes)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "made validate: parse trusted config:", err)
		return 1
	}

	// Load decisions (optional).
	decs, err := readDecisions(opts.DecisionsPath, opts)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "made validate: load decisions:", err)
		return 1
	}

	// Ensure evidence directory exists.
	if mkErr := os.MkdirAll(opts.EvidenceDir, 0o755); mkErr != nil {
		_, _ = fmt.Fprintln(stderr, "made validate: create evidence dir:", mkErr)
		return 1
	}

	ev := &ManagedEvidenceStore{EvidenceDir: opts.EvidenceDir, RunID: opts.RunID}

	// Emit run.started now that preflight succeeded.
	if err := ew.Emit("run.started", RunStartedPayload{}); err != nil {
		_, _ = fmt.Fprintln(stderr, "made validate: emit run.started:", err)
		return 1
	}

	runner := NewRunner(opts, cfg, ew, ev, decs)

	// Run stages.
	outcome, msg, stoppedAt := runner.Run(ctx)

	// Handle context cancellation.
	if ctx.Err() != nil {
		outcome = OutcomeCanceled
		msg = "canceled: " + ctx.Err().Error()
		stoppedAt = "canceled"
	}

	// Write terminal evidence manifest.
	manifest := buildTerminalManifest(opts, runner, outcome, ew.Sequence())
	if writeErr := ev.WriteTerminal(manifest); writeErr != nil {
		_, _ = fmt.Fprintln(stderr, "made validate: write terminal evidence:", writeErr)
		// Do not override outcome; continue to emit terminal event.
	}

	// Report unused decisions to stderr for diagnostics.
	for _, unused := range runner.UnusedDecisions() {
		_, _ = fmt.Fprintf(stderr, "made validate: unused decision %s (fingerprint %s)\n", unused.DecisionID, unused.FindingFingerprint)
	}

	// Emit terminal event.
	if err := ew.EmitTerminal(RunCompletedPayload{
		Outcome:      outcome,
		Stage:        stoppedAt,
		Message:      msg,
		Findings:     runner.AllFindings(),
		EvidenceRefs: runner.EvidenceRefs(),
	}); err != nil {
		_, _ = fmt.Fprintln(stderr, "made validate: emit run.completed:", err)
		return 1
	}

	return outcome.ExitCode()
}
