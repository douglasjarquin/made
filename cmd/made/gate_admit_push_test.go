package main

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/douglasjarquin/made/internal/api"
	"github.com/douglasjarquin/made/internal/daemon"
	"github.com/douglasjarquin/made/internal/gitgate"
)

func startTestDaemon(t *testing.T, home string) (*daemon.RunManager, *api.Client) {
	t.Helper()
	lockPath := filepath.Join(home, "daemon.lock")

	ctx, cancel := context.WithCancel(context.Background())
	ready := make(chan int, 1)
	rm, done := startDaemon(ctx, home, lockPath, time.Minute, func(pid int) { ready <- pid })

	select {
	case <-ready:
	case err := <-done:
		t.Fatalf("daemon exited before becoming ready: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("daemon did not become ready in time")
	}
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("daemon did not shut down after cancel")
		}
	})

	client, err := api.Dial(api.SocketPath(home))
	if err != nil {
		t.Fatalf("dial daemon socket: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return rm, client
}

func TestGateAdmitPushRPC_ValidBareRepoAdmitted(t *testing.T) {
	home := shortTempDir(t)
	_, client := startTestDaemon(t, home)

	barePath := filepath.Join(shortTempDir(t), "gate.git")
	if err := gitgate.InitBare(barePath); err != nil {
		t.Fatalf("InitBare: %v", err)
	}

	if _, err := client.Call("gate.admitPush", gateAdmitPushParams{GatePath: barePath}); err != nil {
		t.Fatalf("gate.admitPush: %v", err)
	}
}

func TestGateAdmitPushRPC_InvalidPathRejected(t *testing.T) {
	home := shortTempDir(t)
	_, client := startTestDaemon(t, home)

	_, err := client.Call("gate.admitPush", gateAdmitPushParams{GatePath: filepath.Join(shortTempDir(t), "does-not-exist")})
	if err == nil {
		t.Fatal("expected gate.admitPush to reject a nonexistent path")
	}
}

func TestGateAdmitPushRPC_NonBareDirRejected(t *testing.T) {
	home := shortTempDir(t)
	_, client := startTestDaemon(t, home)

	notBare := shortTempDir(t)
	_, err := client.Call("gate.admitPush", gateAdmitPushParams{GatePath: notBare})
	if err == nil {
		t.Fatal("expected gate.admitPush to reject a directory that is not a bare repo")
	}
}

func TestGateAdmitPushCLI_ValidGateExitsZero(t *testing.T) {
	home := shortTempDir(t)
	t.Setenv("MADE_HOME", home)
	_, _ = startTestDaemon(t, home)

	barePath := filepath.Join(shortTempDir(t), "gate.git")
	if err := gitgate.InitBare(barePath); err != nil {
		t.Fatalf("InitBare: %v", err)
	}

	out, errOut, code := runCapture(t, []string{"gate", "admit-push", "--gate", barePath})
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stdout=%s stderr=%s", code, out, errOut)
	}
}

func TestGateAdmitPushCLI_InvalidGateExitsNonZero(t *testing.T) {
	home := shortTempDir(t)
	t.Setenv("MADE_HOME", home)
	_, _ = startTestDaemon(t, home)

	out, errOut, code := runCapture(t, []string{"gate", "admit-push", "--gate", "/nonexistent/gate.git"})
	if code == 0 {
		t.Fatalf("expected non-zero exit for invalid gate path, stdout=%s stderr=%s", out, errOut)
	}
	if strings.TrimSpace(string(errOut)) == "" {
		t.Fatal("expected a clear error message on stderr")
	}
}

func TestGateAdmitPushCLI_DaemonUnreachableFailsCleanly(t *testing.T) {
	home := shortTempDir(t)
	t.Setenv("MADE_HOME", home)

	barePath := filepath.Join(shortTempDir(t), "gate.git")
	if err := gitgate.InitBare(barePath); err != nil {
		t.Fatalf("InitBare: %v", err)
	}

	done := make(chan struct{})
	var out, errOut []byte
	var code int
	go func() {
		out, errOut, code = runCapture(t, []string{"gate", "admit-push", "--gate", barePath})
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("made gate admit-push hung with no daemon running")
	}

	if code == 0 {
		t.Fatalf("expected non-zero exit when daemon is unreachable, stdout=%s stderr=%s", out, errOut)
	}
	if strings.TrimSpace(string(errOut)) == "" {
		t.Fatal("expected a clear error message on stderr")
	}
}
