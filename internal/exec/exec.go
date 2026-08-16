package exec

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"syscall"
	"time"
)

type Command struct {
	Name        string
	Args        []string
	Dir         string
	Env         []string
	Stdin       []byte
	Timeout     time.Duration
	OutputLimit int
}

type Result struct {
	ExitCode int
	Stdout   []byte
	Stderr   []byte
}

func Run(ctx context.Context, cmd Command) (*Result, error) {
	if cmd.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, cmd.Timeout)
		defer cancel()
	}

	c := exec.Command(cmd.Name, cmd.Args...)
	c.Dir = cmd.Dir
	c.Env = cmd.Env
	c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	limit := cmd.OutputLimit
	if limit <= 0 {
		limit = defaultOutputLimit
	}
	var stdout, stderr boundedBuffer
	stdout.limit = limit
	stderr.limit = limit
	c.Stdout = &stdout
	c.Stderr = &stderr
	if cmd.Stdin != nil {
		c.Stdin = bytes.NewReader(cmd.Stdin)
	}

	if err := c.Start(); err != nil {
		return nil, fmt.Errorf("start %s: %w", cmd.Name, err)
	}

	waitDone := make(chan error, 1)
	go func() { waitDone <- c.Wait() }()

	select {
	case waitErr := <-waitDone:
		return &Result{
			ExitCode: exitCode(waitErr),
			Stdout:   stdout.Bytes(),
			Stderr:   stderr.Bytes(),
		}, nil
	case <-ctx.Done():
		killGroup(c.Process.Pid)
		<-waitDone
		return &Result{
			ExitCode: -1,
			Stdout:   stdout.Bytes(),
			Stderr:   stderr.Bytes(),
		}, ctx.Err()
	}
}

const defaultOutputLimit = 4 << 20

type boundedBuffer struct {
	data      []byte
	limit     int
	truncated bool
}

func (b *boundedBuffer) Write(data []byte) (int, error) {
	remaining := b.limit - len(b.data)
	if remaining > 0 {
		if len(data) > remaining {
			b.data = append(b.data, data[:remaining]...)
			b.truncated = true
		} else {
			b.data = append(b.data, data...)
		}
	} else if len(data) > 0 {
		b.truncated = true
	}
	return len(data), nil
}

func (b *boundedBuffer) Bytes() []byte {
	data := append([]byte(nil), b.data...)
	if !b.truncated {
		return data
	}
	marker := []byte("\n[output truncated]\n")
	if len(marker) >= b.limit {
		return append([]byte(nil), marker[:b.limit]...)
	}
	data = data[:b.limit-len(marker)]
	return append(data, marker...)
}

// killGroup signals the process's entire group, not just the direct child:
// a negative pid tells the kernel to deliver the signal to every process
// sharing that group ID, which is how a backgrounded grandchild gets reaped
// instead of orphaned.
func killGroup(pid int) {
	_ = syscall.Kill(-pid, syscall.SIGKILL)
}

func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	return -1
}
