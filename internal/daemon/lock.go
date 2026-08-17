package daemon

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

var ErrAlreadyRunning = errors.New("daemon already running")

type Lock struct {
	file *os.File
}

// AcquireLock takes a non-blocking exclusive flock on path, creating the
// file if needed. flock is scoped to the open file description, so the
// kernel drops it automatically when the holder's descriptors close -
// including on a crash - unlike a PID file, whose mere existence proves
// nothing about whether its writer is still alive.
func AcquireLock(path string) (*Lock, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|unix.O_NOFOLLOW, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open lock file %s: %w", path, err)
	}

	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		_ = f.Close()
		if errors.Is(err, unix.EWOULDBLOCK) {
			return nil, ErrAlreadyRunning
		}
		return nil, fmt.Errorf("flock %s: %w", path, err)
	}

	return &Lock{file: f}, nil
}

func (l *Lock) WritePID(pid int) error {
	if err := l.file.Truncate(0); err != nil {
		return fmt.Errorf("truncate lock file: %w", err)
	}
	if _, err := l.file.WriteAt([]byte(fmt.Sprintf("%d\n", pid)), 0); err != nil {
		return fmt.Errorf("write pid: %w", err)
	}
	return nil
}

func (l *Lock) Release() error {
	defer func() { _ = l.file.Close() }()
	return unix.Flock(int(l.file.Fd()), unix.LOCK_UN)
}

func readLockPID(path string) (int, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("read lock file %s: %w", path, err)
	}

	var pid int
	if _, err := fmt.Sscanf(string(data), "%d", &pid); err != nil {
		return 0, nil
	}
	return pid, nil
}
