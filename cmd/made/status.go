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

const statusSchemaVersion = 2

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
	Ref               string                   `json:"ref,omitempty"`
	OldSHA            string                   `json:"old_sha,omitempty"`
	State             string                   `json:"state"`
	SubmissionID      string                   `json:"submission_id,omitempty"`
	GatePath          string                   `json:"gate_path,omitempty"`
	InputSHA          string                   `json:"input_sha"`
	OutputSHA         string                   `json:"output_sha"`
	ExecutionFinished bool                     `json:"execution_finished"`
	CurrentStage      string                   `json:"current_stage,omitempty"`
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
	Message           string                   `json:"message,omitempty"`
	Stages            []StageResult            `json:"stages"`
	PendingFindings   []AskUserFinding         `json:"pending_findings"`
	EvidenceRefs      []string                 `json:"evidence_refs"`
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
			if err := decodeStrictParams(params, &p); err != nil {
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
	byName := make(map[string]StageResult, len(snap.Stages))
	for _, stage := range snap.Stages {
		byName[stage.Name] = stage
	}
	stages := make([]StageResult, len(pipelineStages))
	for i, name := range pipelineStages {
		stage, ok := byName[name]
		if !ok {
			stage = StageResult{Name: name, Result: StageResultPending}
		}
		stages[i] = stage
	}

	pendingFindings := snap.PendingFindings
	if pendingFindings == nil {
		pendingFindings = []AskUserFinding{}
	} else {
		pendingFindings = make([]AskUserFinding, len(snap.PendingFindings))
		for i, finding := range snap.PendingFindings {
			finding.Stage = evidence.RedactString(finding.Stage)
			finding.Message = evidence.RedactString(finding.Message)
			pendingFindings[i] = finding
		}
	}

	errMsg := ""
	if snap.Err != nil {
		errMsg = evidence.RedactString(snap.Err.Error())
	}
	if snap.Error != "" {
		errMsg = evidence.RedactString(snap.Error)
	}
	currentStage := snap.CurrentStage
	if currentStage == "" {
		for _, stage := range snap.Stages {
			if stage.Result != StageResultPass {
				currentStage = stage.Name
				break
			}
		}
	}
	if currentStage == "" {
		for _, stage := range stages {
			if stage.Result != StageResultPass {
				currentStage = stage.Name
				break
			}
		}
	}
	evidenceRefs := append([]string(nil), snap.EvidenceRefs...)
	if evidenceRefs == nil {
		evidenceRefs = []string{}
	}

	return StatusReport{
		SchemaVersion:     statusSchemaVersion,
		ProtocolVersion:   api.Version,
		RunID:             snap.ID,
		Repo:              snap.Repo,
		Branch:            snap.Branch,
		Ref:               snap.Ref,
		OldSHA:            snap.OldSHA,
		State:             string(snap.Status),
		SubmissionID:      snap.SubmissionID,
		GatePath:          snap.GatePath,
		InputSHA:          snap.InputSHA,
		OutputSHA:         snap.OutputSHA,
		ExecutionFinished: snap.ExecutionFinished,
		CurrentStage:      currentStage,
		Findings:          redactedFindings(snap.Findings),
		Decisions:         nonNilDecisions(snap.Decisions),
		PRURL:             evidence.RedactString(snap.PRURL),
		Errors:            redactedErrors(snap.Errors, snap.Err),
		SupersededBy:      snap.SupersededBy,
		CancelRequested:   snap.CancelRequested,
		SubmissionEvents:  nonNilSubmissionEvents(snap.SubmissionEvents),
		QueuedAt:          timePtr(snap.QueuedAt),
		StartedAt:         timePtr(snap.StartedAt),
		EndedAt:           timePtr(snap.EndedAt),
		Error:             errMsg,
		Message:           evidence.RedactString(snap.Message),
		Stages:            stages,
		PendingFindings:   pendingFindings,
		EvidenceRefs:      evidenceRefs,
	}
}

func redactedFindings(findings []daemon.RunFinding) []daemon.RunFinding {
	if findings == nil {
		return []daemon.RunFinding{}
	}
	redacted := make([]daemon.RunFinding, len(findings))
	for i, finding := range findings {
		redacted[i] = finding
		redacted[i].Paths = make([]string, len(finding.Paths))
		for j, path := range finding.Paths {
			redacted[i].Paths[j] = evidence.RedactString(path)
		}
	}
	for i := range redacted {
		redacted[i].Message = evidence.RedactString(redacted[i].Message)
	}
	return redacted
}

func nonNilDecisions(decisions map[string]string) map[string]string {
	if decisions == nil {
		return map[string]string{}
	}
	redacted := make(map[string]string, len(decisions))
	for key, value := range decisions {
		redacted[key] = evidence.RedactString(value)
	}
	return redacted
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
	redacted := make([]daemon.SubmissionEvent, len(events))
	for i, event := range events {
		event.Gate = evidence.RedactString(event.Gate)
		event.Ref = evidence.RedactString(event.Ref)
		event.Kind = evidence.RedactString(event.Kind)
		redacted[i] = event
	}
	return redacted
}

func timePtr(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}
