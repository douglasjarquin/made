package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/douglasjarquin/made/internal/api"
	"github.com/douglasjarquin/made/internal/daemon"
)

type runListParams struct {
	Active bool `json:"active"`
}

type runCancelParams struct {
	RunID string `json:"run_id"`
}

type runCancelResult struct {
	OK bool `json:"ok"`
}

func runSubmitHandler(rm *daemon.RunManager) api.HandlerFunc {
	return func(_ context.Context, params json.RawMessage) (any, error) {
		var submission daemon.RunSubmission
		if err := decodeStrictJSON(params, &submission); err != nil {
			return nil, fmt.Errorf("run.submit: invalid params: %w", err)
		}
		if submission.Repo == "" || submission.Branch == "" {
			return nil, fmt.Errorf("run.submit: repo and branch are required")
		}
		if submission.ID == "" {
			submission.ID = rm.NewRunID()
		}
		if existing, ok := rm.FindSubmission(submission); ok {
			return newStatusReport(existing), nil
		}
		snapshot, err := rm.SubmitSubmission(submission, nil)
		if err != nil {
			return nil, fmt.Errorf("run.submit: %w", err)
		}
		return newStatusReport(snapshot), nil
	}
}

func runListHandler(rm *daemon.RunManager) api.HandlerFunc {
	return func(_ context.Context, params json.RawMessage) (any, error) {
		var p runListParams
		if len(params) > 0 {
			if err := decodeStrictJSON(params, &p); err != nil {
				return nil, fmt.Errorf("run.list: invalid params: %w", err)
			}
		}
		reports := make([]StatusReport, 0)
		for _, snapshot := range rm.List() {
			if p.Active && isTerminalRunStatus(snapshot.Status) {
				continue
			}
			reports = append(reports, newStatusReport(snapshot))
		}
		return reports, nil
	}
}

func runCancelHandler(rm *daemon.RunManager) api.HandlerFunc {
	return func(_ context.Context, params json.RawMessage) (any, error) {
		var p runCancelParams
		if err := decodeStrictJSON(params, &p); err != nil {
			return nil, fmt.Errorf("run.cancel: invalid params: %w", err)
		}
		if p.RunID == "" {
			return nil, fmt.Errorf("run.cancel: run_id is required")
		}
		if err := rm.Cancel(p.RunID); err != nil {
			return nil, err
		}
		return runCancelResult{OK: true}, nil
	}
}

func decodeStrictJSON(data []byte, target any) error {
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return fmt.Errorf("trailing JSON value")
		}
		return fmt.Errorf("trailing JSON: %w", err)
	}
	return nil
}
