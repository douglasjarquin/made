package daemon

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

func TestRun_GracefulStop(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), "daemon.lock")

	ready := make(chan int, 1)
	done := make(chan error, 1)

	go func() {
		done <- Run(context.Background(), Options{
			LockPath:    lockPath,
			IdleTimeout: time.Minute,
			OnReady:     func(pid int) { ready <- pid },
		})
	}()

	select {
	case <-ready:
	case <-time.After(2 * time.Second):
		t.Fatal("daemon did not become ready in time")
	}

	st, err := Status(lockPath)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if !st.Running {
		t.Fatal("expected daemon to report running before stop")
	}

	if err := Stop(lockPath, 2*time.Second); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	select {
	case runErr := <-done:
		if runErr != nil {
			t.Fatalf("Run returned error: %v", runErr)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not exit after Stop")
	}

	st, err = Status(lockPath)
	if err != nil {
		t.Fatalf("Status after stop: %v", err)
	}
	if st.Running {
		t.Fatal("expected daemon to report not running after stop")
	}

	third, err := AcquireLock(lockPath)
	if err != nil {
		t.Fatalf("expected lock to be acquirable by a third party after stop: %v", err)
	}
	_ = third.Release()
}

func TestRun_IdleTimeoutAutoStop(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), "daemon.lock")

	start := time.Now()
	if err := Run(context.Background(), Options{
		LockPath:    lockPath,
		IdleTimeout: 100 * time.Millisecond,
	}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if elapsed := time.Since(start); elapsed < 100*time.Millisecond {
		t.Fatalf("Run exited before idle timeout elapsed: %v", elapsed)
	}

	st, err := Status(lockPath)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if st.Running {
		t.Fatal("expected daemon not running after idle timeout")
	}
}

func TestRun_ActiveRunSurvivesIdleTimeout(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), "daemon.lock")
	rm := NewRunManager()
	const idleTimeout = 200 * time.Millisecond

	done := make(chan error, 1)
	go func() {
		done <- Run(context.Background(), Options{
			LockPath:    lockPath,
			IdleTimeout: idleTimeout,
			ActivityCh:  rm.ActivitySignal(),
		})
	}()

	time.Sleep(20 * time.Millisecond)

	work := func(ctx context.Context, emit func(Event)) error {
		for i := 0; i < 5; i++ {
			time.Sleep(100 * time.Millisecond)
			emit(Event{Kind: EventStageStarted, Stage: fmt.Sprintf("stage-%d", i)})
		}
		return nil
	}
	if _, err := rm.Submit(rm.NewRunID(), "gate-repo-lifecycle", "main", work); err != nil {
		t.Fatalf("submit: %v", err)
	}

	select {
	case err := <-done:
		t.Fatalf("daemon exited early (after %v) while a run was still active with periodic activity: err=%v", idleTimeout, err)
	case <-time.After(500 * time.Millisecond):
	}

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("daemon did not idle-time-out after the run's activity stopped")
	}
}

func TestRun_IdleTimeoutStillFiresWithNoActivity(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), "daemon.lock")
	rm := NewRunManager()
	const idleTimeout = 100 * time.Millisecond

	start := time.Now()
	if err := Run(context.Background(), Options{
		LockPath:    lockPath,
		IdleTimeout: idleTimeout,
		ActivityCh:  rm.ActivitySignal(),
	}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	elapsed := time.Since(start)
	if elapsed < idleTimeout {
		t.Fatalf("Run exited before idle timeout elapsed: %v", elapsed)
	}
	if elapsed > idleTimeout+time.Second {
		t.Fatalf("Run took far longer than the idle timeout to exit: %v", elapsed)
	}

	st, err := Status(lockPath)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if st.Running {
		t.Fatal("expected daemon not running after idle timeout")
	}
}
