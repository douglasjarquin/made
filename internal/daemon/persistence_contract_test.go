package daemon

import (
	"context"
	"testing"
	"time"
)

func TestRunManager_RestoresDurableSnapshotAfterRestart(t *testing.T) {
	stateDir := t.TempDir()
	rm1, err := OpenRunManager(stateDir)
	if err != nil {
		t.Fatalf("OpenRunManager first instance: %v", err)
	}

	release := make(chan struct{})
	submitted, err := rm1.SubmitSubmission(RunSubmission{
		ID:           "run-durable-1",
		Repo:         "example/repo",
		Branch:       "feature/durable",
		Ref:          "refs/heads/feature/durable",
		OldSHA:       "1111111111111111111111111111111111111111",
		InputSHA:     "2222222222222222222222222222222222222222",
		OutputSHA:    "3333333333333333333333333333333333333333",
		SubmissionID: "submission-1",
		GatePath:     "/tmp/made-gate",
	}, func(ctx context.Context, emit func(Event)) error {
		<-release
		return nil
	})
	if err != nil {
		t.Fatalf("SubmitSubmission: %v", err)
	}
	if submitted.Status != RunQueued {
		t.Fatalf("SubmitSubmission returned %q, want pre-drain queued identity", submitted.Status)
	}

	stages := []StageResult{{Name: "intent", Result: "pass"}, {Name: "review", Result: "pending"}}
	if err := rm1.UpdateStages("run-durable-1", stages); err != nil {
		t.Fatalf("UpdateStages: %v", err)
	}
	if err := rm1.Finish("run-durable-1", RunAwaitingMerge, "awaiting human merge"); err != nil {
		t.Fatalf("Finish: %v", err)
	}
	close(release)
	deadline := time.After(2 * time.Second)
	for {
		snap, ok := rm1.Snapshot("run-durable-1")
		if ok && snap.Status == RunAwaitingMerge {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("run did not reach awaiting merge: %+v", snap)
		case <-time.After(5 * time.Millisecond):
		}
	}
	if err := rm1.Close(); err != nil {
		t.Fatalf("Close first instance: %v", err)
	}

	rm2, err := OpenRunManager(stateDir)
	if err != nil {
		t.Fatalf("OpenRunManager after restart: %v", err)
	}
	defer func() { _ = rm2.Close() }()

	restored, ok := rm2.Snapshot("run-durable-1")
	if !ok {
		t.Fatal("run not found after daemon restart")
	}
	if restored.Status != RunAwaitingMerge || restored.Message != "awaiting human merge" {
		t.Fatalf("restored lifecycle = %+v, want awaiting merge", restored)
	}
	if restored.Repo != "example/repo" || restored.Branch != "feature/durable" || restored.Ref != "refs/heads/feature/durable" {
		t.Fatalf("restored submission identity = %+v", restored)
	}
	if restored.InputSHA != "2222222222222222222222222222222222222222" || restored.OutputSHA != "3333333333333333333333333333333333333333" {
		t.Fatalf("restored SHA identity = %+v", restored)
	}
	if restored.SubmissionID != "submission-1" || restored.GatePath != "/tmp/made-gate" {
		t.Fatalf("restored submission metadata = %+v", restored)
	}
	if len(restored.Stages) != len(stages) || restored.Stages[1] != stages[1] {
		t.Fatalf("restored stages = %+v, want %+v", restored.Stages, stages)
	}
}
