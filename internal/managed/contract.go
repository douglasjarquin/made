package managed

import (
	"time"
)

// SchemaVersion is the event envelope schema version.
const SchemaVersion = 1

// ProtocolVersion is the managed-validation protocol version.
const ProtocolVersion = 1

// Outcome represents the terminal validation result.
type Outcome string

const (
	OutcomePassed              Outcome = "passed"
	OutcomeNeedsDecision       Outcome = "needs_decision"
	OutcomeFailedRetryable     Outcome = "failed_retryable"
	OutcomeFailedTerminal      Outcome = "failed_terminal"
	OutcomeInfrastructureError Outcome = "infrastructure_error"
	OutcomeCanceled            Outcome = "canceled"

	// OutcomeNotConfigured and OutcomeDisabled report a stage's coverage
	// state, never a run's terminal outcome: trusted policy configures no
	// command for the stage, or explicitly disables it. Reusing Outcome's
	// "outcome" field for these keeps one vocabulary on stage.completed
	// events instead of a second field a caller would have to reconcile.
	OutcomeNotConfigured Outcome = "not_configured"
	OutcomeDisabled      Outcome = "disabled"

	// OutcomePending marks a stage the plan set to run but that Run never
	// reached because an earlier stage already produced a non-pass outcome.
	// It keeps the stage visible in StageResults instead of silently
	// omitting it.
	OutcomePending Outcome = "pending"
)

// ExitCode returns the process exit code for a given outcome.
func (o Outcome) ExitCode() int {
	switch o {
	case OutcomePassed:
		return 0
	case OutcomeInfrastructureError:
		return 1
	case OutcomeNeedsDecision:
		return 3
	case OutcomeFailedRetryable:
		return 4
	case OutcomeFailedTerminal:
		return 5
	case OutcomeCanceled:
		return 130
	default:
		return 1
	}
}

// Options holds the validated, parsed parameters for a managed-validation run.
type Options struct {
	RunID         string
	MissionID     string
	Workspace     string
	BaseSHA       string
	InputSHA      string
	TrustedConfig string
	PolicyHash    string
	EvidenceDir   string
	DecisionsPath string // optional
	ReviewSource  string // "internal" or "external"; defaults to "internal" when empty
	ReviewResult  string // path to a caller-supplied ExternalReviewResult, required when ReviewSource is "external"

	// InvocationID uniquely identifies a single Run invocation. It is generated
	// internally by Run and used to isolate evidence paths across reruns.
	InvocationID string

	// ReviewAgentBinaryPath overrides the agent binary path for testing.
	// Leave empty in production.
	ReviewAgentBinaryPath string
	// ReviewAgentExtraEnv provides additional env vars for the agent process (testing).
	ReviewAgentExtraEnv []string
}

// Event is one line in the JSON-Lines event stream.
type Event struct {
	SchemaVersion   int       `json:"schema_version"`
	ProtocolVersion int       `json:"protocol_version"`
	Sequence        int       `json:"sequence"`
	RunID           string    `json:"run_id"`
	InvocationID    string    `json:"invocation_id"`
	MissionID       string    `json:"mission_id"`
	InputSHA        string    `json:"input_sha"`
	BaseSHA         string    `json:"base_sha"`
	PolicyHash      string    `json:"policy_hash"`
	EventType       string    `json:"event"`
	Timestamp       time.Time `json:"timestamp"`
	Payload         any       `json:"payload"`
}

// RunStartedPayload is the payload for run.started.
type RunStartedPayload struct{}

// StageStartedPayload is the payload for stage.started.
type StageStartedPayload struct {
	Stage string `json:"stage"`
}

// StageCompletedPayload is the payload for stage.completed.
type StageCompletedPayload struct {
	Stage   string  `json:"stage"`
	Outcome Outcome `json:"outcome"`
	Message string  `json:"message,omitempty"`
}

// FindingReportedPayload is the payload for finding.reported.
type FindingReportedPayload struct {
	Fingerprint string   `json:"fingerprint"`
	Stage       string   `json:"stage"`
	Kind        string   `json:"kind"`
	Code        string   `json:"code,omitempty"`
	Class       string   `json:"class,omitempty"`
	Description string   `json:"description"`
	Paths       []string `json:"paths,omitempty"`
	Symbol      string   `json:"symbol,omitempty"`
	Patch       string   `json:"patch,omitempty"`
}

// EvidenceCreatedPayload is the payload for evidence.created.
type EvidenceCreatedPayload struct {
	Stage string `json:"stage"`
	Path  string `json:"path"`
}

// RunCompletedPayload is the payload for run.completed.
type RunCompletedPayload struct {
	Outcome      Outcome                  `json:"outcome"`
	Stage        string                   `json:"stage"`
	Message      string                   `json:"message"`
	InvocationID string                   `json:"invocation_id,omitempty"`
	Findings     []FindingReportedPayload `json:"findings"`
	EvidenceRefs []string                 `json:"evidence_refs"`
}

// StageResult records the outcome of a single stage.
type StageResult struct {
	Stage    string                   `json:"stage"`
	Outcome  Outcome                  `json:"outcome"`
	Message  string                   `json:"message,omitempty"`
	Findings []FindingReportedPayload `json:"findings"`
}
