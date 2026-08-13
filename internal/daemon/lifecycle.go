package daemon

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"
)

type Options struct {
	LockPath    string
	IdleTimeout time.Duration
	OnReady     func(pid int)
	ActivityCh  <-chan struct{}
}

type StatusInfo struct {
	Running bool
	PID     int
}

func Run(ctx context.Context, opts Options) error {
	lock, err := AcquireLock(opts.LockPath)
	if err != nil {
		return err
	}
	defer func() { _ = lock.Release() }()

	pid := os.Getpid()
	if err := lock.WritePID(pid); err != nil {
		return err
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	var idleCh <-chan time.Time
	var timer *time.Timer
	if opts.IdleTimeout > 0 {
		timer = time.NewTimer(opts.IdleTimeout)
		defer timer.Stop()
		idleCh = timer.C
	}

	if opts.OnReady != nil {
		opts.OnReady(pid)
	}

	for {
		select {
		case <-sigCh:
			return nil
		case <-idleCh:
			return nil
		case <-ctx.Done():
			return nil
		case <-opts.ActivityCh:
			if timer != nil {
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				timer.Reset(opts.IdleTimeout)
			}
		}
	}
}

func Status(lockPath string) (StatusInfo, error) {
	lock, err := AcquireLock(lockPath)
	if err != nil {
		if errors.Is(err, ErrAlreadyRunning) {
			pid, _ := readLockPID(lockPath)
			return StatusInfo{Running: true, PID: pid}, nil
		}
		return StatusInfo{}, err
	}
	defer func() { _ = lock.Release() }()
	return StatusInfo{Running: false}, nil
}

func Stop(lockPath string, timeout time.Duration) error {
	pid, err := readLockPID(lockPath)
	if err != nil {
		return err
	}
	if pid == 0 {
		return errors.New("daemon not running")
	}

	proc, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("find process %d: %w", pid, err)
	}
	if err := proc.Signal(syscall.SIGTERM); err != nil {
		if errors.Is(err, os.ErrProcessDone) {
			return nil
		}
		return fmt.Errorf("signal process %d: %w", pid, err)
	}

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		st, err := Status(lockPath)
		if err == nil && !st.Running {
			return nil
		}
		time.Sleep(20 * time.Millisecond)
	}
	return fmt.Errorf("daemon did not stop within %s", timeout)
}
