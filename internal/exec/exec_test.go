package exec_test

import (
	"context"
	"fmt"
	"os"
	osexec "os/exec"
	"strings"
	"testing"
	"time"

	"github.com/douglasjarquin/made/internal/exec"
)

func TestRunExitCodePropagation(t *testing.T) {
	res, err := exec.Run(context.Background(), exec.Command{
		Name: "sh",
		Args: []string{"-c", "exit 3"},
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if res.ExitCode != 3 {
		t.Fatalf("ExitCode = %d, want 3", res.ExitCode)
	}
}

func TestRunCapturesStderr(t *testing.T) {
	res, err := exec.Run(context.Background(), exec.Command{
		Name: "sh",
		Args: []string{"-c", "echo boom 1>&2; exit 1"},
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if res.ExitCode != 1 {
		t.Fatalf("ExitCode = %d, want 1", res.ExitCode)
	}
	if !strings.Contains(string(res.Stderr), "boom") {
		t.Fatalf("Stderr = %q, want to contain %q", res.Stderr, "boom")
	}
}

func TestRunCancellationReapsGrandchildren(t *testing.T) {
	if _, err := osexec.LookPath("pgrep"); err != nil {
		t.Skip("pgrep not available on this system")
	}

	marker := fmt.Sprintf("%d1187", os.Getpid())

	t.Cleanup(func() {
		_ = osexec.Command("pkill", "-9", "-f", marker).Run()
	})

	ctx, cancel := context.WithCancel(context.Background())
	script := fmt.Sprintf("sleep %s & echo started; wait", marker)

	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = exec.Run(ctx, exec.Command{Name: "sh", Args: []string{"-c", script}})
	}()

	time.Sleep(1 * time.Second)

	beforeOut, err := osexec.Command("pgrep", "-f", marker).CombinedOutput()
	t.Logf("pgrep -f %s (before cancel) => err=%v out=%q", marker, err, string(beforeOut))
	if err != nil || strings.TrimSpace(string(beforeOut)) == "" {
		cancel()
		t.Fatalf("grandchild never started before cancellation: err=%v out=%q", err, beforeOut)
	}

	cancel()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after context cancellation")
	}

	deadline := time.Now().Add(3 * time.Second)
	for {
		out, _ := osexec.Command("pgrep", "-f", marker).CombinedOutput()
		t.Logf("pgrep -f %s (after cancel) => out=%q", marker, string(out))
		if strings.TrimSpace(string(out)) == "" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("grandchild still running after cancellation: pgrep output=%q", out)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func TestRunTimeoutKillsProcessGroup(t *testing.T) {
	if _, err := osexec.LookPath("pgrep"); err != nil {
		t.Skip("pgrep not available on this system")
	}

	marker := fmt.Sprintf("%d2298", os.Getpid())
	t.Cleanup(func() {
		_ = osexec.Command("pkill", "-9", "-f", marker).Run()
	})

	script := fmt.Sprintf("sleep %s", marker)
	res, err := exec.Run(context.Background(), exec.Command{
		Name:    "sh",
		Args:    []string{"-c", script},
		Timeout: 200 * time.Millisecond,
	})
	if err == nil {
		t.Fatalf("expected timeout error, got nil (res=%+v)", res)
	}
	if !strings.Contains(err.Error(), "deadline") && !strings.Contains(err.Error(), "timeout") {
		t.Fatalf("error = %v, want a deadline/timeout error", err)
	}
}
