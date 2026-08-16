package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/douglasjarquin/made/internal/api"
	"github.com/douglasjarquin/made/internal/daemon"
)

func TestRunStateSurvivesDaemonRestart(t *testing.T) {
	t.Setenv(debugHandlersEnv, "1")
	home := shortTempDir(t)
	t.Setenv("MADE_HOME", home)
	firstCtx, firstCancel := context.WithCancel(context.Background())
	firstReady := make(chan int, 1)
	_, firstDone := startDaemon(firstCtx, home, filepath.Join(home, "daemon.lock"), time.Hour, func(pid int) { firstReady <- pid })
	select {
	case <-firstReady:
	case err := <-firstDone:
		t.Fatalf("first daemon stopped: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("first daemon did not become ready")
	}
	client, err := api.Dial(api.SocketPath(home))
	if err != nil {
		t.Fatalf("dial first daemon: %v", err)
	}
	defer func() { _ = client.Close() }()
	runID := "123e4567-e89b-12d3-a456-426614174001"
	var submitted daemon.RunSnapshot
	if err := client.CallInto("debug.submitCancellableRun", map[string]string{
		"id": runID, "repo": "/repo", "branch": "feature",
	}, &submitted); err != nil {
		t.Fatalf("submit debug run: %v", err)
	}
	firstCancel()
	select {
	case <-firstDone:
	case <-time.After(2 * time.Second):
		t.Fatal("first daemon did not stop")
	}

	secondCtx, secondCancel := context.WithCancel(context.Background())
	secondReady := make(chan int, 1)
	_, secondDone := startDaemon(secondCtx, home, filepath.Join(home, "daemon.lock"), time.Hour, func(pid int) { secondReady <- pid })
	t.Cleanup(func() {
		secondCancel()
		select {
		case <-secondDone:
		case <-time.After(2 * time.Second):
			t.Error("second daemon did not stop during cleanup")
		}
	})
	select {
	case <-secondReady:
	case err := <-secondDone:
		t.Fatalf("second daemon stopped: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("second daemon did not become ready")
	}
	client2, err := api.Dial(api.SocketPath(home))
	if err != nil {
		t.Fatalf("dial second daemon: %v", err)
	}
	defer func() { _ = client2.Close() }()
	var status StatusReport
	if err := client2.CallInto("status", statusParams{RunID: runID}, &status); err != nil {
		t.Fatalf("status after restart: %v", err)
	}
	if status.RunID != runID {
		t.Fatalf("status after restart run ID = %q, want %q", status.RunID, runID)
	}
}

func TestStartDaemon_DuplicatePreservesOriginalSocketOwner(t *testing.T) {
	home := shortTempDir(t)
	firstCtx, firstCancel := context.WithCancel(context.Background())
	firstReady := make(chan int, 1)
	_, firstDone := startDaemon(firstCtx, home, filepath.Join(home, "daemon.lock"), time.Hour, func(pid int) { firstReady <- pid })
	select {
	case <-firstReady:
	case err := <-firstDone:
		t.Fatalf("first daemon stopped: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("first daemon did not become ready")
	}
	firstClient, err := api.Dial(api.SocketPath(home))
	if err != nil {
		t.Fatalf("dial first daemon: %v", err)
	}
	t.Cleanup(func() {
		_ = firstClient.Close()
		firstCancel()
		select {
		case <-firstDone:
		case <-time.After(2 * time.Second):
			t.Error("first daemon did not stop during cleanup")
		}
	})
	if _, err := firstClient.Call("ping", nil); err != nil {
		t.Fatalf("first daemon ping: %v", err)
	}

	secondCtx, secondCancel := context.WithCancel(context.Background())
	secondReady := make(chan int, 1)
	_, secondDone := startDaemon(secondCtx, home, filepath.Join(home, "daemon.lock"), time.Hour, func(pid int) { secondReady <- pid })
	secondStopped := false
	t.Cleanup(func() {
		secondCancel()
		if secondStopped {
			return
		}
		select {
		case <-secondDone:
		case <-time.After(2 * time.Second):
			t.Error("duplicate daemon did not stop during cleanup")
		}
	})
	select {
	case err := <-secondDone:
		secondStopped = true
		if !errors.Is(err, daemon.ErrAlreadyRunning) {
			t.Fatalf("duplicate daemon error = %v, want ErrAlreadyRunning", err)
		}
	case <-secondReady:
		secondStopped = true
		t.Fatal("duplicate daemon reported ready and could damage the original owner")
	case <-time.After(2 * time.Second):
		t.Fatal("duplicate daemon did not fail promptly")
	}
	if _, err := firstClient.Call("ping", nil); err != nil {
		t.Fatalf("original daemon became unreachable after duplicate start: %v", err)
	}
}

func TestHermeticCompatibility_RealMadeBinaryThroughConsigliereScript(t *testing.T) {
	root := repoRoot(t)
	consigliereRoot := "/Users/douglasjarquin/github/consigliere"
	script := filepath.Join(consigliereRoot, "bin", "cs-made-lib.sh")
	if _, err := os.Stat(script); err != nil {
		t.Fatalf("real Consigliere script unavailable: %v", err)
	}

	binDir := t.TempDir()
	madePath := filepath.Join(binDir, "made")
	build := exec.Command("go", "build", "-o", madePath, "./cmd/made")
	build.Dir = root
	build.Env = append(os.Environ(), "GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=commit.gpgsign", "GIT_CONFIG_VALUE_0=false")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build Made binary: %v\n%s", err, output)
	}
	fakeGH := filepath.Join(binDir, "gh")
	if err := os.WriteFile(fakeGH, []byte("#!/bin/sh\nset -eu\n[ \"$1\" = auth ] && [ \"$2\" = status ]\nprintf '%s\\n' 'Logged in to github.com account fake'\n"), 0o700); err != nil {
		t.Fatalf("write strict fake gh: %v", err)
	}

	cmd := exec.Command("bash", "-c", `. "$1"; cs_made doctor --json`, "compat", script)
	cmd.Env = append(os.Environ(),
		"MADE_HOME="+t.TempDir(),
		"PATH="+binDir+":"+os.Getenv("PATH"),
	)
	output, err := cmd.CombinedOutput()
	if !json.Valid([]byte(strings.TrimSpace(string(output)))) {
		t.Fatalf("real Consigliere script did not receive a JSON doctor contract: err=%v output=%q", err, output)
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(filename), "../.."))
}
