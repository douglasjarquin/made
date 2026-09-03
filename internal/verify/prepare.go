package verify

import (
	"context"
	"fmt"
	"path/filepath"
)

type PrepareParams struct {
	WorkDir        string
	BaseRef        string
	Executor       string
	RequestedModel string
	Output         string
	TaskFile       string
}

type PrepareOutcome struct {
	Context     ResolvedContext
	Request     Request
	RequestPath string
}

func Prepare(ctx context.Context, p PrepareParams) (PrepareOutcome, error) {
	if p.Executor == "" {
		return PrepareOutcome{}, fmt.Errorf("verify: --executor is required")
	}

	rc, err := ResolveContext(ctx, p.WorkDir, p.BaseRef)
	if err != nil {
		return PrepareOutcome{}, err
	}

	task, err := ReadTaskFile(p.TaskFile)
	if err != nil {
		return PrepareOutcome{}, err
	}

	runID := "verify-" + rc.InputSHA[:12]
	invocationID, err := newInvocationID()
	if err != nil {
		return PrepareOutcome{}, fmt.Errorf("verify: generate invocation id: %w", err)
	}

	requestedModel := p.RequestedModel
	if requestedModel == "" && p.Executor == "cursor" {
		requestedModel = rc.ParsedConfig.Review.Executors.Cursor.Model
	}

	req, err := BuildRequest(rc, runID, invocationID, p.Executor, requestedModel, task)
	if err != nil {
		return PrepareOutcome{}, err
	}

	outputPath := p.Output
	if outputPath == "" {
		outputPath = defaultRequestPath(rc.Repository.Root, invocationID)
	}
	absOutput, err := filepath.Abs(outputPath)
	if err != nil {
		return PrepareOutcome{}, fmt.Errorf("verify: resolve output path: %w", err)
	}
	if err := PublishRequest(absOutput, req); err != nil {
		return PrepareOutcome{}, err
	}

	return PrepareOutcome{Context: rc, Request: req, RequestPath: absOutput}, nil
}

func defaultRequestPath(root, invocationID string) string {
	return filepath.Join(RequestsDir(StateRoot(root)), invocationID+".json")
}
