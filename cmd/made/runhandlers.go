package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
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

func runSubmitHandler(rm *daemon.RunManager, reviewDecisions *daemon.ReviewDecisions, spool *daemon.GateSpool, admission ...*sync.Mutex) api.HandlerFunc {
	return func(ctx context.Context, params json.RawMessage) (any, error) {
		var p runSubmitParams
		if err := decodeStrictParams(params, &p); err != nil {
			return nil, fmt.Errorf("run.submit: invalid params: %w", err)
		}
		if strings.TrimSpace(p.GatePath) == "" || strings.TrimSpace(p.Ref) == "" {
			return nil, fmt.Errorf("run.submit: gate_path and ref are required to execute a gate pipeline")
		}
		branch, ok := strings.CutPrefix(p.Ref, "refs/heads/")
		if !ok || branch == "" {
			return nil, fmt.Errorf("run.submit: ref must be a non-empty refs/heads branch")
		}
		if p.Branch != "" && p.Branch != branch {
			return nil, fmt.Errorf("run.submit: branch %q does not match ref %q", p.Branch, p.Ref)
		}
		if p.Repo != "" && p.Repo != gateRepoIdentifier(p.GatePath) {
			return nil, fmt.Errorf("run.submit: repo %q does not match the gate identity", p.Repo)
		}
		if !validSHA(p.InputSHA) || (p.OutputSHA != "" && !validSHA(p.OutputSHA)) || (p.OldSHA != "" && !validSHA(p.OldSHA)) {
			return nil, fmt.Errorf("run.submit: input_sha, old_sha, and optional output_sha must use valid 40-character SHAs")
		}
		if p.OldSHA == "" {
			p.OldSHA = gitZeroSHAValue
		}
		if reviewDecisions == nil || spool == nil {
			return nil, fmt.Errorf("run.submit: executable gate dependencies are unavailable")
		}
		request, err := json.Marshal(gateNotifyPushParams{
			GatePath:  p.GatePath,
			OldSHA:    p.OldSHA,
			NewSHA:    p.InputSHA,
			Ref:       p.Ref,
			RunID:     p.RunID,
			OutputSHA: p.OutputSHA,
		})
		if err != nil {
			return nil, fmt.Errorf("run.submit: encode gate request: %w", err)
		}
		result, err := gateNotifyPushHandler(rm, reviewDecisions, spool, admission...)(ctx, request)
		if err != nil {
			return nil, err
		}
		gateResult, ok := result.(gateNotifyPushResult)
		if !ok {
			return nil, fmt.Errorf("run.submit: unexpected gate submission response %T", result)
		}
		if gateResult.RunID == "" {
			return nil, fmt.Errorf("run.submit: gate submission did not create a run")
		}
		snapshot, ok := rm.Snapshot(gateResult.RunID)
		if !ok {
			return nil, fmt.Errorf("run.submit: submitted run %q was not persisted", gateResult.RunID)
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
			if err := decodeStrictParams(params, &p); err != nil {
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
		if err := decodeStrictParams(params, &p); err != nil {
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

func daemonShutdownHandler(rm *daemon.RunManager, spool *daemon.GateSpool, cancel context.CancelFunc, admission ...*sync.Mutex) api.HandlerFunc {
	return func(_ context.Context, params json.RawMessage) (any, error) {
		var noParams struct{}
		if err := decodeStrictParams(params, &noParams); err != nil {
			return nil, fmt.Errorf("daemon.shutdown: invalid params: %w", err)
		}
		unlock := lockAdmission(admission)
		defer unlock()
		if spool.HasPending() {
			return nil, fmt.Errorf("daemon.shutdown: active or awaiting runs remain")
		}
		if err := rm.BeginShutdown(); err != nil {
			return nil, fmt.Errorf("daemon.shutdown: active or awaiting runs remain: %w", err)
		}
		cancel()
		return map[string]any{"ok": true, "schema_version": 1, "protocol_version": api.Version}, nil
	}
}

func lockAdmission(admission []*sync.Mutex) func() {
	if len(admission) == 0 || admission[0] == nil {
		return func() {}
	}
	admission[0].Lock()
	return admission[0].Unlock
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
