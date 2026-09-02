//go:build !windows

package config

import (
	"syscall"
	"time"
)

func mkfifo(path string) error {
	return syscall.Mkfifo(path, 0o644)
}

func timeoutChan() <-chan time.Time {
	return time.After(2 * time.Second)
}
