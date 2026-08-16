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
	Name    string
	Args    []string
	Dir     string
	Env     []string
	Stdin   []byte
	Timeout time.Duration
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

	var stdout, stderr bytes.Buffer
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
