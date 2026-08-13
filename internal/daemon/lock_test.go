package daemon

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestAcquireLock_DoubleStartRejected(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), "daemon.lock")

	first, err := AcquireLock(lockPath)
	if err != nil {
		t.Fatalf("first AcquireLock: %v", err)
	}
	t.Cleanup(func() { _ = first.Release() })

	_, err = AcquireLock(lockPath)
	if !errors.Is(err, ErrAlreadyRunning) {
		t.Fatalf("second AcquireLock: got %v, want ErrAlreadyRunning", err)
	}
}

func TestAcquireLock_ReacquireAfterRelease(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), "daemon.lock")

	first, err := AcquireLock(lockPath)
	if err != nil {
		t.Fatalf("first AcquireLock: %v", err)
	}
	if err := first.Release(); err != nil {
		t.Fatalf("Release: %v", err)
	}

	second, err := AcquireLock(lockPath)
	if err != nil {
		t.Fatalf("second AcquireLock after release: %v", err)
	}
	_ = second.Release()
}
