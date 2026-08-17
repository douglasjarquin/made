package daemon

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
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
	if len(restored.Stages) != len(stages) || !reflect.DeepEqual(restored.Stages[1], stages[1]) {
		t.Fatalf("restored stages = %+v, want %+v", restored.Stages, stages)
	}
	if err := rm2.Finish("run-durable-1", RunSucceeded, "merged"); err != nil {
		t.Fatalf("Finish succeeded: %v", err)
	}
	finished, _ := rm2.Snapshot("run-durable-1")
	if finished.Status != RunSucceeded || !finished.ExecutionFinished {
		t.Fatalf("awaiting_merge did not transition to succeeded: %+v", finished)
	}
}

func TestRunManager_IgnoresTornFinalWALRecord(t *testing.T) {
	stateDir := t.TempDir()
	rm, err := OpenRunManager(stateDir)
	if err != nil {
		t.Fatalf("OpenRunManager: %v", err)
	}
	if _, err := rm.Submit("run-torn-tail", "repo", "branch", func(context.Context, func(Event)) error { return nil }); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		snapshot, _ := rm.Snapshot("run-torn-tail")
		if snapshot.Status == RunSucceeded {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if err := rm.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	wal, err := os.OpenFile(filepath.Join(stateDir, walFileName), os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		t.Fatalf("open WAL: %v", err)
	}
	if _, err := wal.WriteString(`{"snapshot":{"run_id":"run-torn-tail"`); err != nil {
		t.Fatalf("append torn WAL: %v", err)
	}
	_ = wal.Close()

	restarted, err := OpenRunManager(stateDir)
	if err != nil {
		t.Fatalf("OpenRunManager after torn tail: %v", err)
	}
	defer func() { _ = restarted.Close() }()
	if snapshot, ok := restarted.Snapshot("run-torn-tail"); !ok || snapshot.Status != RunSucceeded {
		t.Fatalf("valid checkpoint was lost with torn WAL tail: %+v (ok=%v)", snapshot, ok)
	}
}

func TestRunManager_WALRetentionIsBounded(t *testing.T) {
	stateDir := t.TempDir()
	rm, err := OpenRunManager(stateDir)
	if err != nil {
		t.Fatalf("OpenRunManager: %v", err)
	}
	if _, err := rm.Submit("run-retention", "repo", "branch", func(context.Context, func(Event)) error { return nil }); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	for i := 0; i < maxWALRecords+10; i++ {
		if err := rm.UpdateStages("run-retention", []StageResult{{Name: "stage", Result: "pass", Message: "update"}}); err != nil {
			t.Fatalf("UpdateStages %d: %v", i, err)
		}
	}
	if info, err := os.Stat(filepath.Join(stateDir, walFileName)); err != nil {
		t.Fatalf("stat WAL: %v", err)
	} else if info.Size() >= maxWALBytes {
		t.Fatalf("WAL exceeded retention bound: %d bytes", info.Size())
	}
	if err := rm.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestReviewDecisions_RestoreAndRejectConflict(t *testing.T) {
	stateDir := t.TempDir()
	rm, err := OpenRunManager(stateDir)
	if err != nil {
		t.Fatalf("OpenRunManager: %v", err)
	}
	if _, err := rm.Submit("run-decision", "repo", "branch", func(context.Context, func(Event)) error { return nil }); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		snapshot, _ := rm.Snapshot("run-decision")
		if snapshot.Status == RunSucceeded {
			break
		}
		time.Sleep(time.Millisecond)
	}
	decisions := NewReviewDecisionsForManager(rm)
	if err := decisions.Set("run-decision", "review", ReviewRejected); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := rm.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	restarted, err := OpenRunManager(stateDir)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer func() { _ = restarted.Close() }()
	restoredDecisions := NewReviewDecisionsForManager(restarted)
	decision, ok := restoredDecisions.Get("run-decision", "review")
	if !ok || decision != ReviewRejected {
		t.Fatalf("decision did not restore: %q (ok=%v)", decision, ok)
	}
	if err := restoredDecisions.Set("run-decision", "review", ReviewApproved); !errors.Is(err, ErrDecisionAlreadyRecorded) {
		t.Fatalf("conflicting decision error = %v, want ErrDecisionAlreadyRecorded", err)
	}
}

func TestRunManager_UpdateStagesRollsBackOnPersistenceFailure(t *testing.T) {
	stateDir := t.TempDir()
	rm, err := OpenRunManager(stateDir)
	if err != nil {
		t.Fatalf("OpenRunManager: %v", err)
	}
	if _, err := rm.SubmitSubmission(RunSubmission{
		ID:     "run-persist-failure",
		Repo:   "repo",
		Branch: "branch",
	}, nil); err != nil {
		t.Fatalf("SubmitSubmission: %v", err)
	}
	if err := rm.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	err = rm.UpdateStages("run-persist-failure", []StageResult{{Name: "intent", Result: "pass"}})
	if err == nil {
		t.Fatal("UpdateStages succeeded with a closed durable store")
	}
	snapshot, ok := rm.Snapshot("run-persist-failure")
	if !ok {
		t.Fatal("run disappeared after persistence failure")
	}
	if len(snapshot.Stages) != 0 {
		t.Fatalf("in-memory stage update survived persistence failure: %+v", snapshot.Stages)
	}
}

func TestRunManager_FailsRunWhenFinalPersistenceFails(t *testing.T) {
	stateDir := t.TempDir()
	rm, err := OpenRunManager(stateDir)
	if err != nil {
		t.Fatalf("OpenRunManager: %v", err)
	}
	if _, err := rm.SubmitSubmission(RunSubmission{
		ID:     "run-final-persist-failure",
		Repo:   "repo",
		Branch: "branch",
	}, func(context.Context, func(Event)) error {
		if err := rm.Close(); err != nil {
			return err
		}
		return nil
	}); err != nil {
		t.Fatalf("SubmitSubmission: %v", err)
	}

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		snapshot, ok := rm.Snapshot("run-final-persist-failure")
		if ok && snapshot.ExecutionFinished {
			if snapshot.Status != RunFailed {
				t.Fatalf("run status = %s after final persistence failure, want failed", snapshot.Status)
			}
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("run did not finish")
}
