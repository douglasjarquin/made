package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/douglasjarquin/made/internal/api"
	"github.com/douglasjarquin/made/internal/daemon"
)

type runActionReport struct {
	SchemaVersion   int    `json:"schema_version"`
	ProtocolVersion int    `json:"protocol_version"`
	RunID           string `json:"run_id"`
	State           string `json:"state"`
	InputSHA        string `json:"input_sha"`
	OutputSHA       string `json:"output_sha"`
}

func runStatusHandler(rm *daemon.RunManager) api.HandlerFunc {
	return func(ctx context.Context, params json.RawMessage) (any, error) {
		return statusHandler(rm)(ctx, params)
	}
}

func runSubmitHandler(rm *daemon.RunManager) api.HandlerFunc {
	return func(_ context.Context, params json.RawMessage) (any, error) {
		var p runSubmitParams
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, fmt.Errorf("run.submit: invalid params: %w", err)
		}
		if strings.TrimSpace(p.Repo) == "" || strings.TrimSpace(p.Branch) == "" || !validSHA(p.InputSHA) || (p.OutputSHA != "" && !validSHA(p.OutputSHA)) {
			return nil, fmt.Errorf("run.submit: repo, branch, input_sha, and optional output_sha must use valid 40-character SHAs")
		}
		if p.RunID == "" {
			p.RunID = rm.NewRunID()
		}
		snapshot, err := rm.SubmitWithMetadata(p.RunID, p.Repo, p.Branch, p.InputSHA, p.OutputSHA, func(ctx context.Context, _ func(daemon.Event)) error {
			<-ctx.Done()
			return ctx.Err()
		})
		if err != nil {
			return nil, err
		}
		return runActionReport{
			SchemaVersion: 1, ProtocolVersion: api.Version, RunID: snapshot.ID,
			State: string(snapshot.Status), InputSHA: snapshot.InputSHA, OutputSHA: snapshot.OutputSHA,
		}, nil
	}
}

func runListHandler(rm *daemon.RunManager) api.HandlerFunc {
	return func(_ context.Context, params json.RawMessage) (any, error) {
		var p runListParams
		if len(params) > 0 {
			if err := json.Unmarshal(params, &p); err != nil {
				return nil, fmt.Errorf("run.list: invalid params: %w", err)
			}
		}
		runs := rm.List()
		report := runListReport{SchemaVersion: 1, ProtocolVersion: api.Version, Runs: make([]StatusReport, 0, len(runs))}
		for _, snapshot := range runs {
			if p.Active && !activeRunStatus(snapshot.Status) {
				continue
			}
			report.Runs = append(report.Runs, newStatusReport(snapshot))
		}
		return report, nil
	}
}

func runCancelHandler(rm *daemon.RunManager) api.HandlerFunc {
	return func(ctx context.Context, params json.RawMessage) (any, error) {
		var p runCancelParams
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, fmt.Errorf("run.cancel: invalid params: %w", err)
		}
		if p.RunID == "" {
			return nil, fmt.Errorf("run.cancel: run_id is required")
		}
		if err := rm.Cancel(p.RunID); err != nil {
			return nil, err
		}
		waitCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		for {
			snapshot, ok := rm.Snapshot(p.RunID)
			if !ok {
				return nil, fmt.Errorf("run.cancel: no run %q", p.RunID)
			}
			if snapshot.Status == daemon.RunCanceled && snapshot.ExecutionFinished {
				return runActionReport{SchemaVersion: 1, ProtocolVersion: api.Version, RunID: p.RunID, State: string(snapshot.Status), InputSHA: snapshot.InputSHA, OutputSHA: snapshot.OutputSHA}, nil
			}
			select {
			case <-waitCtx.Done():
				return nil, fmt.Errorf("run.cancel: cancellation of %q did not finish: %w", p.RunID, waitCtx.Err())
			case <-time.After(5 * time.Millisecond):
			}
		}
	}
}

func daemonShutdownHandler(rm *daemon.RunManager, spool *daemon.GateSpool, cancel context.CancelFunc) api.HandlerFunc {
	return func(_ context.Context, _ json.RawMessage) (any, error) {
		if rm.HasActive() || spool.HasPending() {
			return nil, fmt.Errorf("daemon.shutdown: active or awaiting runs remain")
		}
		cancel()
		return map[string]any{"ok": true, "schema_version": 1, "protocol_version": api.Version}, nil
	}
}

func activeRunStatus(status daemon.RunStatus) bool {
	switch status {
	case daemon.RunQueued, daemon.RunRunning, daemon.RunAwaitingReview, daemon.RunAwaitingMerge:
		return true
	default:
		return false
	}
}

func validSHA(value string) bool {
	if len(value) != 40 {
		return false
	}
	for _, r := range value {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') && (r < 'A' || r > 'F') {
			return false
		}
	}
	return true
}

func writeJSON(stdout *os.File, value any, stderr *os.File, label string) int {
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		_, _ = fmt.Fprintln(stderr, label+":", err)
		return 1
	}
	return 0
}
