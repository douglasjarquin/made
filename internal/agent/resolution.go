package agent

import "time"

// AttemptReason names why a candidate kind was skipped during resolution.
type AttemptReason string

const (
	ReasonMissing         AttemptReason = "missing"
	ReasonUnauthenticated AttemptReason = "unauthenticated"
	ReasonQuotaExhausted  AttemptReason = "quota_exhausted"
)

// CandidateAttempt records one skipped candidate and why.
type CandidateAttempt struct {
	Kind          Kind          `json:"kind"`
	Reason        AttemptReason `json:"reason,omitempty"`
	QuotaResetsAt *time.Time    `json:"quota_resets_at,omitempty"`
}

// AgentResolution is the one shared shape for both outcomes of resolving an
// agent candidate list (project: agent auto-resolve, decision D18): on
// success, Selected names the kind that was used, and Attempts lists only
// the candidates skipped before it; on exhaustion, Selected is nil and
// Attempts names every candidate tried and why each failed - never a
// generic error, so a caller can recognize this specific, structured case.
type AgentResolution struct {
	Selected *Kind              `json:"selected,omitempty"`
	Attempts []CandidateAttempt `json:"attempts"`
}

// AllExhausted reports whether every candidate in Attempts failed.
func (r AgentResolution) AllExhausted() bool {
	return r.Selected == nil
}
