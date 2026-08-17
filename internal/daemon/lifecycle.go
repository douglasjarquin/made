package daemon

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"syscall"
	"time"
)

type Options struct {
	LockPath      string
	Lock          *Lock
	IdleTimeout   time.Duration
	OnReady       func(pid int)
	ActivityCh    <-chan struct{}
	ActiveFunc    func() bool
	UndrainedFunc func() bool
}

type StatusInfo struct {
	Running bool
	PID     int
}

func Run(ctx context.Context, opts Options) error {
	lock := opts.Lock
	if lock == nil {
		var err error
		lock, err = AcquireLock(opts.LockPath)
		if err != nil {
			return err
		}
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
			if opts.ActiveFunc != nil && opts.ActiveFunc() {
				timer.Reset(opts.IdleTimeout)
				continue
			}
			if opts.UndrainedFunc != nil && opts.UndrainedFunc() {
				timer.Reset(opts.IdleTimeout)
				continue
			}
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
	return errors.New("daemon: shutdown requires the owner-only socket")
}
