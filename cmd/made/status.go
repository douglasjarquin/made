package main

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/douglasjarquin/made/internal/api"
	"github.com/douglasjarquin/made/internal/daemon"
	"github.com/douglasjarquin/made/internal/evidence"
)

const statusSchemaVersion = 1

const (
	StageResultPass    = "pass"
	StageResultFail    = "fail"
	StageResultPending = "pending"
)

// pipelineStages lists the 9-stage pipeline in execution order. Every run
// reports one StageResult per stage regardless of how far it actually got,
// so a consumer (Task 26's consigliere migration) can always index by name.
var pipelineStages = []string{
	"intent", "rebase", "review", "test", "document", "lint", "push", "pr", "ci",
}

// StatusReport is the schema for `made run status --json`. Stages and
// PendingFindings come straight from daemon.RunSnapshot; until an orchestrator
// calls UpdateStages/UpdatePendingFindings on a run, Stages falls back to an
// all-"pending" list over the fixed 9-stage order and PendingFindings falls
// back to empty.
type StatusReport struct {
	SchemaVersion     int                      `json:"schema_version"`
	ProtocolVersion   int                      `json:"protocol_version"`
	RunID             string                   `json:"run_id"`
	Repo              string                   `json:"repo"`
	Branch            string                   `json:"branch"`
	State             string                   `json:"state"`
	InputSHA          string                   `json:"input_sha"`
	OutputSHA         string                   `json:"output_sha"`
	ExecutionFinished bool                     `json:"execution_finished"`
	Findings          []daemon.RunFinding      `json:"findings"`
	Decisions         map[string]string        `json:"decisions"`
	PRURL             string                   `json:"pr_url"`
	Errors            []string                 `json:"errors"`
	SupersededBy      string                   `json:"superseded_by"`
	CancelRequested   bool                     `json:"cancel_requested"`
	SubmissionEvents  []daemon.SubmissionEvent `json:"submission_events"`
	QueuedAt          *time.Time               `json:"queued_at,omitempty"`
	StartedAt         *time.Time               `json:"started_at,omitempty"`
	EndedAt           *time.Time               `json:"ended_at,omitempty"`
	Error             string                   `json:"error,omitempty"`
	Stages            []StageResult            `json:"stages"`
	PendingFindings   []AskUserFinding         `json:"pending_findings"`
}

type StageResult = daemon.StageResult

type AskUserFinding = daemon.AskUserFinding

type statusParams struct {
	RunID string `json:"run_id"`
}

func statusHandler(rm *daemon.RunManager) api.HandlerFunc {
	return func(ctx context.Context, params json.RawMessage) (any, error) {
		var p statusParams
		if len(params) > 0 {
			if err := json.Unmarshal(params, &p); err != nil {
				return nil, fmt.Errorf("status: invalid params: %w", err)
			}
		}

		if p.RunID == "" {
			return nil, fmt.Errorf("run.status: run_id is required")
		}
		snap, ok := rm.Snapshot(p.RunID)
		if !ok {
			return nil, fmt.Errorf("run.status: exact run_id %q was not found", p.RunID)
		}
		return newStatusReport(snap), nil
	}
}

func newStatusReport(snap daemon.RunSnapshot) StatusReport {
	stages := snap.Stages
	if len(stages) == 0 {
		stages = make([]StageResult, len(pipelineStages))
		for i, name := range pipelineStages {
			stages[i] = StageResult{Name: name, Result: StageResultPending}
		}
	}

	pendingFindings := snap.PendingFindings
	if pendingFindings == nil {
		pendingFindings = []AskUserFinding{}
	}

	errMsg := ""
	if snap.Err != nil {
		errMsg = evidence.RedactString(snap.Err.Error())
	}

	return StatusReport{
		SchemaVersion:     statusSchemaVersion,
		ProtocolVersion:   api.Version,
		RunID:             snap.ID,
		Repo:              snap.Repo,
		Branch:            snap.Branch,
		State:             string(snap.Status),
		InputSHA:          snap.InputSHA,
		OutputSHA:         snap.OutputSHA,
		ExecutionFinished: snap.ExecutionFinished,
		Findings:          redactedFindings(snap.Findings),
		Decisions:         nonNilDecisions(snap.Decisions),
		PRURL:             snap.PRURL,
		Errors:            redactedErrors(snap.Errors, snap.Err),
		SupersededBy:      snap.SupersededBy,
		CancelRequested:   snap.CancelRequested,
		SubmissionEvents:  nonNilSubmissionEvents(snap.SubmissionEvents),
		QueuedAt:          timePtr(snap.QueuedAt),
		StartedAt:         timePtr(snap.StartedAt),
		EndedAt:           timePtr(snap.EndedAt),
		Error:             errMsg,
		Stages:            stages,
		PendingFindings:   pendingFindings,
	}
}

func redactedFindings(findings []daemon.RunFinding) []daemon.RunFinding {
	if findings == nil {
		return []daemon.RunFinding{}
	}
	redacted := make([]daemon.RunFinding, len(findings))
	copy(redacted, findings)
	for i := range redacted {
		redacted[i].Message = evidence.RedactString(redacted[i].Message)
	}
	return redacted
}

func nonNilDecisions(decisions map[string]string) map[string]string {
	if decisions == nil {
		return map[string]string{}
	}
	return decisions
}

func redactedErrors(values []string, runErr error) []string {
	if len(values) == 0 {
		if runErr == nil {
			return []string{}
		}
		return []string{evidence.RedactString(runErr.Error())}
	}
	redacted := make([]string, len(values))
	for i, value := range values {
		redacted[i] = evidence.RedactString(value)
	}
	return redacted
}

func nonNilSubmissionEvents(events []daemon.SubmissionEvent) []daemon.SubmissionEvent {
	if events == nil {
		return []daemon.SubmissionEvent{}
	}
	return events
}

func timePtr(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}
