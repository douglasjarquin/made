package main

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"time"

	"github.com/douglasjarquin/made/internal/api"
	"github.com/douglasjarquin/made/internal/daemon"
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

// StatusReport is the schema for `made status --json`, replacing
// no-mistakes' TOON status output for downstream consumers (Task 26). Stages
// and PendingFindings come straight from daemon.RunSnapshot; until an
// orchestrator (this plan's Task 12) actually calls UpdateStages/
// UpdatePendingFindings on a run, Stages falls back to an all-"pending" list
// over the fixed 9-stage order and PendingFindings falls back to empty, so
// callers can integrate against the shape before real orchestration lands.
type StatusReport struct {
	SchemaVersion     int               `json:"schema_version"`
	RunID             string            `json:"run_id"`
	Repo              string            `json:"repo"`
	Branch            string            `json:"branch"`
	Ref               string            `json:"ref,omitempty"`
	OldSHA            string            `json:"old_sha,omitempty"`
	InputSHA          string            `json:"input_sha,omitempty"`
	OutputSHA         string            `json:"output_sha,omitempty"`
	SubmissionID      string            `json:"submission_id,omitempty"`
	GatePath          string            `json:"gate_path,omitempty"`
	State             string            `json:"state"`
	ExecutionFinished bool              `json:"execution_finished"`
	CurrentStage      string            `json:"current_stage,omitempty"`
	QueuedAt          *time.Time        `json:"queued_at,omitempty"`
	StartedAt         *time.Time        `json:"started_at,omitempty"`
	EndedAt           *time.Time        `json:"ended_at,omitempty"`
	Error             string            `json:"error,omitempty"`
	Message           string            `json:"message,omitempty"`
	Stages            []StageResult     `json:"stages"`
	PendingFindings   []AskUserFinding  `json:"pending_findings"`
	EvidenceRefs      []string          `json:"evidence_refs"`
	Decisions         map[string]string `json:"decisions"`
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
			if err := decodeStrictJSON(params, &p); err != nil {
				return nil, fmt.Errorf("status: invalid params: %w", err)
			}
		}
		if p.RunID == "" {
			return nil, fmt.Errorf("status: run_id is required")
		}

		snap, ok := resolveRun(rm, p.RunID)
		if !ok {
			return nil, fmt.Errorf("status: no run %q", p.RunID)
		}
		return newStatusReport(snap), nil
	}
}

func resolveRun(rm *daemon.RunManager, runID string) (daemon.RunSnapshot, bool) {
	if runID != "" {
		return rm.Snapshot(runID)
	}
	runs := rm.List()
	if len(runs) == 0 {
		return daemon.RunSnapshot{}, false
	}
	latest := runs[0]
	for _, r := range runs[1:] {
		if r.QueuedAt.After(latest.QueuedAt) {
			latest = r
		}
	}
	return latest, true
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
	}

	errMsg := ""
	if snap.Err != nil {
		errMsg = snap.Err.Error()
	}
	if snap.Error != "" {
		errMsg = snap.Error
	}
	currentStage := snap.CurrentStage
	if currentStage == "" {
		for _, stage := range snap.Stages {
			if stage.Result != StageResultPass {
				currentStage = stage.Name
				break
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
	}
	evidenceRefs := append([]string{}, snap.EvidenceRefs...)
	decisions := map[string]string{}
	maps.Copy(decisions, snap.Decisions)

	return StatusReport{
		SchemaVersion:     statusSchemaVersion,
		RunID:             snap.ID,
		Repo:              snap.Repo,
		Branch:            snap.Branch,
		Ref:               snap.Ref,
		OldSHA:            snap.OldSHA,
		InputSHA:          snap.InputSHA,
		OutputSHA:         snap.OutputSHA,
		SubmissionID:      snap.SubmissionID,
		GatePath:          snap.GatePath,
		State:             string(snap.Status),
		ExecutionFinished: snap.ExecutionFinished,
		CurrentStage:      currentStage,
		QueuedAt:          timePtr(snap.QueuedAt),
		StartedAt:         timePtr(snap.StartedAt),
		EndedAt:           timePtr(snap.EndedAt),
		Error:             errMsg,
		Message:           snap.Message,
		Stages:            stages,
		PendingFindings:   pendingFindings,
		EvidenceRefs:      evidenceRefs,
		Decisions:         decisions,
	}
}

func timePtr(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}
