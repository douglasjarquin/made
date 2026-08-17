package main

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/douglasjarquin/made/internal/api"
)

func buildMadeBinary(t *testing.T) string {
	t.Helper()
	binPath := filepath.Join(t.TempDir(), "made")
	cmd := exec.Command("go", "build", "-o", binPath, ".")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build made binary: %v\n%s", err, out)
	}
	return binPath
}

func TestDaemonStop_CancelsInFlightRunBeforeProcessExits(t *testing.T) {
	if testing.Short() {
		t.Skip("subprocess-level daemon test skipped in -short mode")
	}

	binPath := buildMadeBinary(t)
	home := shortTempDir(t)
	sideEffectFile := filepath.Join(t.TempDir(), "cancelled.txt")
	env := append(os.Environ(), "MADE_HOME="+home, "MADE_DEBUG_HANDLERS=1")

	startCmd := exec.Command(binPath, "daemon", "start", "--idle-timeout=5m")
	startCmd.Env = env
	stdoutPipe, err := startCmd.StdoutPipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	startCmd.Stderr = os.Stderr

	if err := startCmd.Start(); err != nil {
		t.Fatalf("start daemon: %v", err)
	}
	t.Cleanup(func() {
		if startCmd.ProcessState == nil {
			_ = startCmd.Process.Kill()
		}
	})

	readyLine := make(chan string, 1)
	go func() {
		scanner := bufio.NewScanner(stdoutPipe)
		if scanner.Scan() {
			readyLine <- scanner.Text()
		}
		for scanner.Scan() {
		}
		if err := scanner.Err(); err != nil {
			return
		}
	}()

	select {
	case line := <-readyLine:
		if !strings.Contains(line, "started") {
			t.Fatalf("unexpected daemon start output: %q", line)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("daemon did not report ready in time")
	}

	socketPath := api.SocketPath(home)
	var client *api.Client
	for range 200 {
		client, err = api.Dial(socketPath)
		if err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if client == nil {
		t.Fatalf("dial daemon socket: %v", err)
	}
	defer func() { _ = client.Close() }()

	const runID = "e2e-cancel-run"
	if err := client.CallInto("debug.submitCancellableRun", map[string]string{
		"id":               runID,
		"repo":             "gate-repo-e2e",
		"branch":           "main",
		"side_effect_file": sideEffectFile,
	}, nil); err != nil {
		t.Fatalf("submit long run: %v", err)
	}

	deadline := time.Now().Add(10 * time.Second)
	for {
		var report StatusReport
		if err := client.CallInto("run.status", statusParams{RunID: runID}, &report); err != nil {
			t.Fatalf("status: %v", err)
		}
		if report.State == "running" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("run never reached running state, last state %q", report.State)
		}
		time.Sleep(20 * time.Millisecond)
	}

	stopCmd := exec.Command(binPath, "daemon", "stop")
	stopCmd.Env = env
	if out, err := stopCmd.CombinedOutput(); err != nil {
		t.Fatalf("daemon stop failed: %v\n%s", err, out)
	}

	waitErr := make(chan error, 1)
	go func() { waitErr <- startCmd.Wait() }()

	select {
	case err := <-waitErr:
		if err != nil {
			t.Fatalf("daemon start process exited with error: %v", err)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("daemon start process did not exit after daemon stop")
	}

	data, err := os.ReadFile(sideEffectFile)
	if err != nil {
		t.Fatalf("WorkFunc never wrote its side effect file before the daemon process exited: %v", err)
	}
	if string(data) != "cancelled" {
		t.Fatalf("unexpected side effect file contents: %q", data)
	}
}
