package daemon

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestRunManager_CancelQueuedRunNeverStartsWork(t *testing.T) {
	rm := NewRunManager()
	started := make(chan struct{})
	release := make(chan struct{})
	blockerID := rm.NewRunID()
	if _, err := rm.Submit(blockerID, "repo-cancel-queued", "blocker", func(ctx context.Context, emit func(Event)) error {
		<-release
		return nil
	}); err != nil {
		t.Fatalf("submit blocker: %v", err)
	}
	waitForStatus(t, rm, blockerID, RunRunning, time.Second)

	var sideEffects atomic.Int32
	queuedID := rm.NewRunID()
	if _, err := rm.Submit(queuedID, "repo-cancel-queued", "feature", func(ctx context.Context, emit func(Event)) error {
		close(started)
		sideEffects.Add(1)
		return nil
	}); err != nil {
		t.Fatalf("submit queued run: %v", err)
	}
	if err := rm.Cancel(queuedID); err != nil {
		t.Fatalf("cancel queued run: %v", err)
	}
	close(release)

	select {
	case <-started:
		t.Fatal("cancelled queued run started execution")
	case <-time.After(150 * time.Millisecond):
	}

	snap, ok := rm.Snapshot(queuedID)
	if !ok {
		t.Fatal("cancelled queued run disappeared")
	}
	if snap.Status == RunRunning || snap.Status == RunCompleted {
		t.Fatalf("cancelled queued run reached executable state: %+v", snap)
	}
	if sideEffects.Load() != 0 {
		t.Fatalf("cancelled queued run performed %d side effects", sideEffects.Load())
	}
}

func TestRunManager_AwaitingMergeDoesNotEmitTerminalCompletion(t *testing.T) {
	rm := NewRunManager()
	runID := rm.NewRunID()
	events, unsubscribe := rm.Subscribe(runID)
	defer unsubscribe()
	if _, err := rm.Submit(runID, "repo-awaiting-merge", "feature", func(ctx context.Context, emit func(Event)) error {
		if err := rm.Finish(runID, RunAwaitingMerge, "all stages passed, PR open, awaiting merge"); err != nil {
			return err
		}
		return nil
	}); err != nil {
		t.Fatalf("submit: %v", err)
	}

	select {
	case ev := <-events:
		if ev.Kind == EventRunCompleted || ev.Kind == EventRunFailed {
			t.Fatalf("awaiting-merge emitted terminal event: %+v", ev)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for run start event")
	}
	select {
	case ev := <-events:
		if ev.Kind == EventRunCompleted || ev.Kind == EventRunFailed {
			t.Fatalf("awaiting-merge emitted terminal event: %+v", ev)
		}
	case <-time.After(100 * time.Millisecond):
	}

	snap, ok := rm.Snapshot(runID)
	if !ok || snap.Status != RunAwaitingMerge {
		t.Fatalf("awaiting-merge status = %+v (ok=%v), want awaiting_merge", snap, ok)
	}
}

func TestRunManager_SnapshotDoesNotAliasStageSlices(t *testing.T) {
	rm := NewRunManager()
	runID := rm.NewRunID()
	if _, err := rm.Submit(runID, "repo-alias", "feature", func(ctx context.Context, emit func(Event)) error {
		<-ctx.Done()
		return ctx.Err()
	}); err != nil {
		t.Fatalf("submit: %v", err)
	}

	stages := []StageResult{{Name: "intent", Result: "pass"}}
	if err := rm.UpdateStages(runID, stages); err != nil {
		t.Fatalf("UpdateStages: %v", err)
	}
	stages[0].Result = "fail"
	snap, _ := rm.Snapshot(runID)
	if snap.Stages[0].Result != "pass" {
		t.Fatalf("snapshot stages aliased caller memory: %+v", snap.Stages)
	}

	snap.Stages[0].Result = "fail"
	fresh, _ := rm.Snapshot(runID)
	if fresh.Stages[0].Result != "pass" {
		t.Fatalf("snapshot stages exposed internal memory: %+v", fresh.Stages)
	}
	if err := rm.Cancel(runID); err != nil && !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel cleanup: %v", err)
	}
}

func TestReviewDecisions_FirstDecisionWins(t *testing.T) {
	d := NewReviewDecisions()
	if err := d.Set("run-conflict", "review", ReviewRejected); err != nil {
		t.Fatalf("first decision: %v", err)
	}
	if err := d.Set("run-conflict", "review", ReviewApproved); err == nil {
		t.Fatal("expected conflicting decision to be rejected")
	}
	decision, ok := d.Get("run-conflict", "review")
	if !ok {
		t.Fatal("expected first decision to be recorded")
	}
	if decision != ReviewRejected {
		t.Fatalf("conflicting decision overwrote first decision: got %q", decision)
	}
}
