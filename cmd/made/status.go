package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/douglasjarquin/made/internal/api"
	"github.com/douglasjarquin/made/internal/daemon"
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

// StatusReport is the schema for `made status --json`, replacing
// no-mistakes' TOON status output for downstream consumers (Task 26). Stages
// and PendingFindings come straight from daemon.RunSnapshot; until an
// orchestrator (this plan's Task 12) actually calls UpdateStages/
// UpdatePendingFindings on a run, Stages falls back to an all-"pending" list
// over the fixed 9-stage order and PendingFindings falls back to empty, so
// callers can integrate against the shape before real orchestration lands.
type StatusReport struct {
	SchemaVersion   int              `json:"schema_version"`
	RunID           string           `json:"run_id"`
	Repo            string           `json:"repo"`
	Branch          string           `json:"branch"`
	State           string           `json:"state"`
	QueuedAt        *time.Time       `json:"queued_at,omitempty"`
	StartedAt       *time.Time       `json:"started_at,omitempty"`
	EndedAt         *time.Time       `json:"ended_at,omitempty"`
	Error           string           `json:"error,omitempty"`
	Stages          []StageResult    `json:"stages"`
	PendingFindings []AskUserFinding `json:"pending_findings"`
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

		snap, ok := resolveRun(rm, p.RunID)
		if !ok {
			if p.RunID != "" {
				return nil, fmt.Errorf("status: no run %q", p.RunID)
			}
			return nil, fmt.Errorf("status: no runs found")
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
		errMsg = snap.Err.Error()
	}

	return StatusReport{
		SchemaVersion:   statusSchemaVersion,
		RunID:           snap.ID,
		Repo:            snap.Repo,
		Branch:          snap.Branch,
		State:           string(snap.Status),
		QueuedAt:        timePtr(snap.QueuedAt),
		StartedAt:       timePtr(snap.StartedAt),
		EndedAt:         timePtr(snap.EndedAt),
		Error:           errMsg,
		Stages:          stages,
		PendingFindings: pendingFindings,
	}
}

func timePtr(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

func runStatusCommand(args []string, stdout, stderr *os.File) int {
	fs := flag.NewFlagSet("made status", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "output structured JSON matching the StatusReport schema")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	runID := ""
	if fs.NArg() > 0 {
		runID = fs.Arg(0)
	}

	home, err := madeHome()
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "made status:", err)
		return 1
	}

	client, err := api.Dial(api.SocketPath(home))
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "made status: daemon not reachable:", err)
		return 1
	}
	defer func() { _ = client.Close() }()

	var report StatusReport
	if err := client.CallInto("status", statusParams{RunID: runID}, &report); err != nil {
		_, _ = fmt.Fprintln(stderr, "made status:", err)
		return 1
	}

	if *asJSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(report); err != nil {
			_, _ = fmt.Fprintln(stderr, "made status:", err)
			return 1
		}
		return 0
	}

	_, _ = fmt.Fprintf(stdout, "run:      %s\n", report.RunID)
	_, _ = fmt.Fprintf(stdout, "repo:     %s\n", report.Repo)
	_, _ = fmt.Fprintf(stdout, "branch:   %s\n", report.Branch)
	_, _ = fmt.Fprintf(stdout, "state:    %s\n", report.State)
	if report.Error != "" {
		_, _ = fmt.Fprintf(stdout, "error:    %s\n", report.Error)
	}
	_, _ = fmt.Fprintln(stdout, "stages:")
	for _, s := range report.Stages {
		_, _ = fmt.Fprintf(stdout, "  %-10s %s\n", s.Name+":", s.Result)
	}
	if len(report.PendingFindings) == 0 {
		_, _ = fmt.Fprintln(stdout, "findings: none pending")
	} else {
		_, _ = fmt.Fprintln(stdout, "findings:")
		for _, f := range report.PendingFindings {
			_, _ = fmt.Fprintf(stdout, "  [%s] %s\n", f.Stage, f.Message)
		}
	}
	return 0
}
