package pipeline

import (
	"context"
	"log/slog"

	"github.com/douglasjarquin/made/internal/herdrclient"
)

// Visibility is a best-effort live pane in herdr for a gate run. herdr is
// never a trust boundary or execution channel for made (see
// internal/herdrclient's package doc): every failure path in Open and Close
// degrades to a no-op instead of returning an error, so a caller's pass/fail
// outcome can never depend on herdr being reachable, protocol-compatible, or
// working. Callers never need to check availability themselves - a zero-value
// Visibility (nil pane) behaves identically to one backed by a live pane.
//
// herdrclient's Pane exposes only Tail (read) and Close, with no write-side
// method to push arbitrary text into a pane from outside. So Visibility opens
// a pane in herdr for presence (something to attach to and watch), but does
// not tee command output into it - that would require a write-into-pane
// primitive herdrclient does not currently expose.
type Visibility struct {
	pane *herdrclient.Pane
}

// Open connects to herdr and opens a pane labeled with runID. Any failure
// along the way - herdr not running, an incompatible protocol version, or
// the pane open call itself failing - is logged as a fail-open warning and
// Open still returns a usable, no-op Visibility.
func Open(ctx context.Context, runID string) *Visibility {
	res := herdrclient.Connect(ctx)
	if !res.Available() {
		slog.Warn("herdrview: herdr unavailable, proceeding without a live pane",
			"run_id", runID, "state", res.State.String())
		return &Visibility{}
	}

	pane, err := res.Client.OpenPane(ctx, herdrclient.OpenPaneOptions{Label: runID})
	if err != nil {
		slog.Warn("herdrview: failed to open pane, proceeding without a live pane",
			"run_id", runID, "error", err)
		return &Visibility{}
	}

	return &Visibility{pane: pane}
}

// Close closes the pane opened by Open, if any. It is a safe no-op when no
// pane was opened, and it never propagates an error to the caller: closing
// the pane is a courtesy to herdr's UI, not part of the run's outcome.
func (v *Visibility) Close(ctx context.Context) {
	if v == nil || v.pane == nil {
		return
	}
	if err := v.pane.Close(ctx); err != nil {
		slog.Warn("herdrview: failed to close pane", "error", err)
	}
}
