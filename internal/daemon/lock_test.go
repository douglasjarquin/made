package daemon

import (
	"errors"
	"os"
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

func TestStateFilesRejectSymlinkChildren(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	if err := os.WriteFile(target, []byte("keep"), 0o600); err != nil {
		t.Fatalf("write target: %v", err)
	}
	cases := []struct {
		name string
		open func(string) error
	}{
		{name: "lock", open: func(path string) error {
			lock, err := AcquireLock(path)
			if lock != nil {
				_ = lock.Release()
			}
			return err
		}},
		{name: "wal", open: func(path string) error {
			_, _, err := OpenRunStore(path)
			return err
		}},
		{name: "spool", open: func(path string) error {
			_, err := OpenGateSpool(path)
			return err
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			link := filepath.Join(dir, tc.name)
			if err := os.Symlink(target, link); err != nil {
				t.Fatalf("create symlink: %v", err)
			}
			if err := tc.open(link); err == nil {
				t.Fatal("state opener followed a symlink child")
			}
			data, err := os.ReadFile(target)
			if err != nil {
				t.Fatalf("read target: %v", err)
			}
			if string(data) != "keep" {
				t.Fatalf("symlink target was modified: %q", data)
			}
		})
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
