package daemon

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestRunManager_SequentialQueuing(t *testing.T) {
	rm := NewRunManager()
	const repo = "gate-repo-A"

	var active int32
	var overlapped int32

	work := func(ctx context.Context, emit func(Event)) error {
		if atomic.AddInt32(&active, 1) > 1 {
			atomic.StoreInt32(&overlapped, 1)
		}
		time.Sleep(50 * time.Millisecond)
		atomic.AddInt32(&active, -1)
		return nil
	}

	id1 := rm.NewRunID()
	id2 := rm.NewRunID()

	if _, err := rm.Submit(id1, repo, "branch-a", work); err != nil {
		t.Fatalf("submit run1: %v", err)
	}
	if _, err := rm.Submit(id2, repo, "branch-b", work); err != nil {
		t.Fatalf("submit run2: %v", err)
	}

	immediate, ok := rm.Snapshot(id2)
	if !ok {
		t.Fatal("run2 not tracked immediately after submit")
	}
	if immediate.Status != RunQueued {
		t.Fatalf("expected run2 queued while run1 is active, got %v", immediate.Status)
	}

	deadline := time.After(2 * time.Second)
	for {
		s2, ok := rm.Snapshot(id2)
		if ok && (s2.Status == RunSucceeded || s2.Status == RunFailed) {
			break
		}
		select {
		case <-deadline:
			t.Fatal("timed out waiting for run2 to complete")
		case <-time.After(5 * time.Millisecond):
		}
	}

	if atomic.LoadInt32(&overlapped) != 0 {
		t.Fatal("run1 and run2 executed concurrently, expected per-repo serialization")
	}

	s1, _ := rm.Snapshot(id1)
	s2, _ := rm.Snapshot(id2)
	if s2.StartedAt.Before(s1.EndedAt) {
		t.Fatalf("run2 started (%v) before run1 ended (%v)", s2.StartedAt, s1.EndedAt)
	}
}

func TestRunManager_DifferentRepposRunConcurrently(t *testing.T) {
	rm := NewRunManager()

	release := make(chan struct{})
	started := make(chan struct{}, 2)

	work := func(ctx context.Context, emit func(Event)) error {
		started <- struct{}{}
		<-release
		return nil
	}

	id1 := rm.NewRunID()
	id2 := rm.NewRunID()

	if _, err := rm.Submit(id1, "gate-repo-B1", "main", work); err != nil {
		t.Fatalf("submit run1: %v", err)
	}
	if _, err := rm.Submit(id2, "gate-repo-B2", "main", work); err != nil {
		t.Fatalf("submit run2: %v", err)
	}

	for i := 0; i < 2; i++ {
		select {
		case <-started:
		case <-time.After(2 * time.Second):
			t.Fatal("timed out waiting for both runs (different repos) to start concurrently")
		}
	}
	close(release)
}

func TestRunManager_StatusStreamCompleteness(t *testing.T) {
	rm := NewRunManager()
	const repo = "gate-repo-C"

	id := rm.NewRunID()
	events, unsubscribe := rm.Subscribe(id)
	defer unsubscribe()

	work := func(ctx context.Context, emit func(Event)) error {
		emit(Event{Kind: EventStageStarted, Stage: "intent"})
		emit(Event{Kind: EventStageFinished, Stage: "intent"})
		emit(Event{Kind: EventStageStarted, Stage: "rebase"})
		emit(Event{Kind: EventStageFinished, Stage: "rebase"})
		emit(Event{Kind: EventStageStarted, Stage: "build"})
		emit(Event{Kind: EventStageFinished, Stage: "build"})
		return nil
	}

	if _, err := rm.Submit(id, repo, "feature-x", work); err != nil {
		t.Fatalf("submit: %v", err)
	}

	wantKinds := []EventKind{
		EventRunStarted,
		EventStageStarted, EventStageFinished,
		EventStageStarted, EventStageFinished,
		EventStageStarted, EventStageFinished,
		EventRunCompleted,
	}
	wantStages := []string{"", "intent", "intent", "rebase", "rebase", "build", "build", ""}

	for i, wantKind := range wantKinds {
		select {
		case ev := <-events:
			if ev.Kind != wantKind {
				t.Fatalf("event %d: got kind %q, want %q", i, ev.Kind, wantKind)
			}
			if ev.Stage != wantStages[i] {
				t.Fatalf("event %d: got stage %q, want %q", i, ev.Stage, wantStages[i])
			}
			if ev.RunID != id {
				t.Fatalf("event %d: got RunID %q, want %q", i, ev.RunID, id)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("timed out waiting for event %d (%s) - an event was dropped or never emitted", i, wantKind)
		}
	}

	select {
	case ev := <-events:
		t.Fatalf("unexpected extra event after the expected sequence: %+v", ev)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestRunManager_FailedWorkEmitsRunFailed(t *testing.T) {
	rm := NewRunManager()
	id := rm.NewRunID()
	events, unsubscribe := rm.Subscribe(id)
	defer unsubscribe()

	boom := errAssertion("boom")
	if _, err := rm.Submit(id, "gate-repo-D", "main", func(ctx context.Context, emit func(Event)) error {
		return boom
	}); err != nil {
		t.Fatalf("submit: %v", err)
	}

	<-events
	final := <-events
	if final.Kind != EventRunFailed {
		t.Fatalf("got kind %q, want %q", final.Kind, EventRunFailed)
	}
	if final.Err != boom {
		t.Fatalf("got err %v, want %v", final.Err, boom)
	}

	snap, ok := rm.Snapshot(id)
	if !ok || snap.Status != RunFailed {
		t.Fatalf("got snapshot %+v, want status RunFailed", snap)
	}
}

type errAssertion string

func (e errAssertion) Error() string { return string(e) }

func TestRunManager_DuplicateRunIDRejected(t *testing.T) {
	rm := NewRunManager()
	id := rm.NewRunID()
	noop := func(ctx context.Context, emit func(Event)) error { return nil }

	if _, err := rm.Submit(id, "gate-repo-E", "main", noop); err != nil {
		t.Fatalf("first submit: %v", err)
	}
	if _, err := rm.Submit(id, "gate-repo-E", "main", noop); err == nil {
		t.Fatal("expected error resubmitting the same run ID, got nil")
	}
}

func waitForStatus(t *testing.T, rm *RunManager, id string, want RunStatus, timeout time.Duration) RunSnapshot {
	t.Helper()
	deadline := time.After(timeout)
	for {
		s, ok := rm.Snapshot(id)
		if ok && s.Status == want {
			return s
		}
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for run %q to reach status %q (last seen %+v)", id, want, s)
		case <-time.After(5 * time.Millisecond):
		}
	}
}

func TestRunManager_CancelStopsRunningWorkFunc(t *testing.T) {
	rm := NewRunManager()
	id := rm.NewRunID()
	unblocked := make(chan struct{})

	work := func(ctx context.Context, emit func(Event)) error {
		<-ctx.Done()
		close(unblocked)
		return ctx.Err()
	}

	if _, err := rm.Submit(id, "gate-repo-cancel", "main", work); err != nil {
		t.Fatalf("submit: %v", err)
	}

	waitForStatus(t, rm, id, RunRunning, 2*time.Second)

	if err := rm.Cancel(id); err != nil {
		t.Fatalf("Cancel: %v", err)
	}

	select {
	case <-unblocked:
	case <-time.After(time.Second):
		t.Fatal("WorkFunc did not unblock within 1s of Cancel")
	}

	final := waitForStatus(t, rm, id, RunCanceled, 2*time.Second)
	if final.Err == nil || !errors.Is(final.Err, context.Canceled) {
		t.Fatalf("expected final error to wrap context.Canceled, got %v", final.Err)
	}
}

func TestRunManager_CancelUnknownRunErrors(t *testing.T) {
	rm := NewRunManager()
	if err := rm.Cancel("no-such-run"); err == nil {
		t.Fatal("expected error cancelling an unknown run ID")
	}
}

func TestRunManager_SupersedeQueuedDropsOnlyStillQueuedJobForBranch(t *testing.T) {
	rm := NewRunManager()
	const repo = "gate-repo-supersede"

	blockStarted := make(chan struct{})
	blockRelease := make(chan struct{})
	blockID := rm.NewRunID()
	if _, err := rm.Submit(blockID, repo, "blocker-branch", func(ctx context.Context, emit func(Event)) error {
		close(blockStarted)
		<-blockRelease
		return nil
	}); err != nil {
		t.Fatalf("submit blocker: %v", err)
	}
	<-blockStarted

	var mu sync.Mutex
	var ran []string
	recordWork := func(label string) WorkFunc {
		return func(ctx context.Context, emit func(Event)) error {
			mu.Lock()
			ran = append(ran, label)
			mu.Unlock()
			return nil
		}
	}

	id1 := rm.NewRunID()
	if _, err := rm.Submit(id1, repo, "feature-x", recordWork("first")); err != nil {
		t.Fatalf("submit first: %v", err)
	}

	if snap, ok := rm.Snapshot(id1); !ok || snap.Status != RunQueued {
		t.Fatalf("expected first run still queued behind the blocker before supersession, got %+v (ok=%v)", snap, ok)
	}

	if err := rm.SupersedeQueued(repo, "feature-x"); err != nil {
		t.Fatalf("SupersedeQueued: %v", err)
	}

	id2 := rm.NewRunID()
	if _, err := rm.Submit(id2, repo, "feature-x", recordWork("second")); err != nil {
		t.Fatalf("submit second: %v", err)
	}

	close(blockRelease)

	waitForStatus(t, rm, id2, RunSucceeded, 2*time.Second)

	final1, ok := rm.Snapshot(id1)
	if !ok {
		t.Fatal("expected superseded run to remain tracked, not deleted")
	}
	if final1.Status != RunSuperseded {
		t.Fatalf("expected superseded run status RunSuperseded, got %v", final1.Status)
	}
	if !errors.Is(final1.Err, ErrRunSuperseded) {
		t.Fatalf("expected superseded run's error to wrap ErrRunSuperseded, got %v", final1.Err)
	}
	if !final1.StartedAt.IsZero() {
		t.Fatal("superseded run must never have started")
	}

	mu.Lock()
	defer mu.Unlock()
	if len(ran) != 1 || ran[0] != "second" {
		t.Fatalf("expected only the second job's WorkFunc to run, got %v", ran)
	}
}

func TestRunManager_SupersedeQueuedLeavesAlreadyStartedRunAlone(t *testing.T) {
	rm := NewRunManager()
	const repo = "gate-repo-supersede-started"

	started := make(chan struct{})
	release := make(chan struct{})
	id := rm.NewRunID()
	if _, err := rm.Submit(id, repo, "feature-x", func(ctx context.Context, emit func(Event)) error {
		close(started)
		<-release
		return nil
	}); err != nil {
		t.Fatalf("submit: %v", err)
	}

	<-started
	waitForStatus(t, rm, id, RunRunning, time.Second)

	if err := rm.SupersedeQueued(repo, "feature-x"); err != nil {
		t.Fatalf("SupersedeQueued: %v", err)
	}

	if snap, _ := rm.Snapshot(id); snap.Status != RunRunning {
		t.Fatalf("expected already-started run to stay RunRunning after SupersedeQueued, got %v", snap.Status)
	}

	close(release)
	final := waitForStatus(t, rm, id, RunSucceeded, 2*time.Second)
	if final.Err != nil {
		t.Fatalf("expected already-started run to complete normally, got err %v", final.Err)
	}
}

func TestRunManager_CancelTerminalRunErrors(t *testing.T) {
	rm := NewRunManager()
	id := rm.NewRunID()
	noop := func(ctx context.Context, emit func(Event)) error { return nil }
	if _, err := rm.Submit(id, "gate-repo-cancel-terminal", "main", noop); err != nil {
		t.Fatalf("submit: %v", err)
	}

	waitForStatus(t, rm, id, RunSucceeded, 2*time.Second)

	if err := rm.Cancel(id); err == nil {
		t.Fatal("expected error cancelling an already-terminal run")
	}
}
