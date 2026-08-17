package main

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/douglasjarquin/made/internal/daemon"
)

func TestRunSubmitSpoolsWithoutClaimingExecution(t *testing.T) {
	rm := daemon.NewRunManager()
	params, err := json.Marshal(daemon.RunSubmission{
		ID:           "run-spooled",
		Repo:         "repo",
		Branch:       "feature",
		InputSHA:     "input-sha",
		SubmissionID: "submission",
	})
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}
	result, err := runSubmitHandler(rm)(context.Background(), params)
	if err != nil {
		t.Fatalf("run.submit: %v", err)
	}
	report, ok := result.(StatusReport)
	if !ok {
		t.Fatalf("run.submit result type = %T, want StatusReport", result)
	}
	if report.State != string(daemon.RunQueued) || report.ExecutionFinished {
		t.Fatalf("run.submit claimed execution: %+v", report)
	}
	time.Sleep(50 * time.Millisecond)
	snapshot, ok := rm.Snapshot("run-spooled")
	if !ok || snapshot.Status != daemon.RunQueued || snapshot.ExecutionFinished {
		t.Fatalf("spooled run changed without refresh: %+v (ok=%v)", snapshot, ok)
	}
}

func TestRunSubmitRefreshAttachesExecutionWork(t *testing.T) {
	rm := daemon.NewRunManager()
	if _, err := rm.SubmitSubmission(daemon.RunSubmission{
		ID:       "run-refresh",
		Repo:     "repo",
		Branch:   "feature",
		InputSHA: "input-sha",
	}, nil); err != nil {
		t.Fatalf("spool submission: %v", err)
	}
	if err := rm.RefreshQueued("run-refresh", func(context.Context, func(daemon.Event)) error {
		return nil
	}); err != nil {
		t.Fatalf("refresh queued: %v", err)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		snapshot, _ := rm.Snapshot("run-refresh")
		if snapshot.Status == daemon.RunSucceeded {
			return
		}
		time.Sleep(time.Millisecond)
	}
	snapshot, _ := rm.Snapshot("run-refresh")
	t.Fatalf("refreshed run did not execute: %+v", snapshot)
}
