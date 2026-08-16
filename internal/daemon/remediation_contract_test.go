package daemon

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"
)

func TestRunManager_SuccessUsesSucceededLifecycle(t *testing.T) {
	rm := NewRunManager()
	id := rm.NewRunID()
	events, unsubscribe := rm.Subscribe(id)
	defer unsubscribe()

	if _, err := rm.Submit(id, "repo-success", "feature", func(context.Context, func(Event)) error {
		return nil
	}); err != nil {
		t.Fatalf("Submit: %v", err)
	}

	for {
		select {
		case event := <-events:
			if event.Kind == EventRunCompleted {
				snapshot, ok := rm.Snapshot(id)
				if !ok {
					t.Fatal("completed run disappeared from durable query surface")
				}
				if snapshot.Status != RunStatus("succeeded") {
					t.Fatalf("state = %q, want succeeded", snapshot.Status)
				}
				if snapshot.EndedAt.IsZero() {
					t.Fatal("execution-finished timestamp was not recorded separately from lifecycle state")
				}
				return
			}
		case <-time.After(2 * time.Second):
			t.Fatal("timed out waiting for completed execution event")
		}
	}
}

func TestRunManager_CancelUsesCanceledLifecycleAndIsIdempotent(t *testing.T) {
	rm := NewRunManager()
	id := rm.NewRunID()
	started := make(chan struct{})
	if _, err := rm.Submit(id, "repo-cancel", "feature", func(ctx context.Context, _ func(Event)) error {
		close(started)
		<-ctx.Done()
		return ctx.Err()
	}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	<-started

	if err := rm.Cancel(id); err != nil {
		t.Fatalf("first Cancel: %v", err)
	}
	if err := rm.Cancel(id); err != nil {
		t.Fatalf("second Cancel must be idempotent: %v", err)
	}

	deadline := time.After(2 * time.Second)
	for {
		snapshot, ok := rm.Snapshot(id)
		if ok && snapshot.Status == RunStatus("canceled") {
			return
		}
		select {
		case <-deadline:
			snapshot, _ := rm.Snapshot(id)
			t.Fatalf("state = %q, want canceled", snapshot.Status)
		case <-time.After(5 * time.Millisecond):
		}
	}
}

func TestRunManager_SupersedeUsesSupersededLifecycle(t *testing.T) {
	rm := NewRunManager()
	const repo = "repo-supersession"
	release := make(chan struct{})
	started := make(chan struct{})
	blocker := rm.NewRunID()
	if _, err := rm.Submit(blocker, repo, "blocker", func(context.Context, func(Event)) error {
		close(started)
		<-release
		return nil
	}); err != nil {
		t.Fatalf("Submit blocker: %v", err)
	}
	<-started

	first := rm.NewRunID()
	if _, err := rm.Submit(first, repo, "feature", func(context.Context, func(Event)) error { return nil }); err != nil {
		t.Fatalf("Submit first: %v", err)
	}
	rm.SupersedeQueued(repo, "feature")
	close(release)

	deadline := time.After(2 * time.Second)
	for {
		snapshot, ok := rm.Snapshot(first)
		if ok && snapshot.Status == RunStatus("superseded") {
			if !errors.Is(snapshot.Err, ErrRunSuperseded) {
				t.Fatalf("superseded error = %v, want ErrRunSuperseded", snapshot.Err)
			}
			return
		}
		select {
		case <-deadline:
			snapshot, _ := rm.Snapshot(first)
			t.Fatalf("state = %q, want superseded", snapshot.Status)
		case <-time.After(5 * time.Millisecond):
		}
	}
}

func TestRunManager_NewRunIDIsRestartSafeUUID(t *testing.T) {
	rm := NewRunManager()
	first := rm.NewRunID()
	second := rm.NewRunID()
	if first == "run-1" || second == "run-2" || first == second {
		t.Fatalf("run IDs are restart/order-derived: %q and %q", first, second)
	}
	if len(first) != 36 || len(second) != 36 {
		t.Fatalf("run IDs must be UUID-shaped, got %q and %q", first, second)
	}
}

func TestRun_DoesNotIdleStopWithAwaitingMergeRun(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), "daemon.lock")
	rm := NewRunManager()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	stopped := false
	t.Cleanup(func() {
		cancel()
		if stopped {
			return
		}
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Error("daemon did not stop during cleanup")
		}
	})
	go func() {
		done <- Run(ctx, Options{
			LockPath:    lockPath,
			IdleTimeout: 100 * time.Millisecond,
			ActivityCh:  rm.ActivitySignal(),
			ActiveFunc:  rm.HasActive,
		})
	}()

	id := rm.NewRunID()
	if _, err := rm.Submit(id, "repo-awaiting-merge", "feature", func(context.Context, func(Event)) error {
		if err := rm.Finish(id, RunStatus("awaiting_merge"), "PR is open"); err != nil {
			return err
		}
		return nil
	}); err != nil {
		t.Fatalf("Submit: %v", err)
	}

	deadline := time.After(2 * time.Second)
	for {
		snapshot, ok := rm.Snapshot(id)
		if ok && snapshot.Status == RunStatus("awaiting_merge") {
			break
		}
		select {
		case <-deadline:
			t.Fatal("run did not reach awaiting_merge")
		case <-time.After(5 * time.Millisecond):
		}
	}

	select {
	case err := <-done:
		stopped = true
		t.Fatalf("daemon idled out while awaiting merge: %v", err)
	case <-time.After(250 * time.Millisecond):
	}
}

func TestStop_RefusesUnownedStalePID(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), "daemon.lock")
	process := exec.Command("sleep", "60")
	if err := process.Start(); err != nil {
		t.Fatalf("start owner probe: %v", err)
	}
	t.Cleanup(func() {
		_ = process.Process.Kill()
		_ = process.Wait()
	})
	if err := os.WriteFile(lockPath, []byte(strconv.Itoa(process.Process.Pid)+"\n"), 0o600); err != nil {
		t.Fatalf("write stale PID fixture: %v", err)
	}

	if err := Stop(lockPath, 100*time.Millisecond); err == nil {
		t.Fatal("Stop trusted an unowned stale PID and signaled the unrelated process")
	}
	if err := process.Process.Signal(syscall.Signal(0)); err != nil {
		t.Fatalf("stale PID target was terminated by unowned shutdown: %v", err)
	}
}
