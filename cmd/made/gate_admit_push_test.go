package main

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/douglasjarquin/made/internal/api"
	"github.com/douglasjarquin/made/internal/daemon"
	"github.com/douglasjarquin/made/internal/gitgate"
)

// testDaemonClient dials the daemon socket for every call. The server closes a
// connection that stays idle for api's request read timeout (one second), and
// tests routinely spend longer than that on git fixtures between Dial and the
// first Call, which made a long-lived client flake with "broken pipe" under
// full-suite load. Real callers dial and call back to back, so this mirrors
// them rather than holding one connection open across fixture setup.
type testDaemonClient struct {
	socketPath string
}

func (c testDaemonClient) Call(method string, params any) (json.RawMessage, error) {
	client, err := api.Dial(c.socketPath)
	if err != nil {
		return nil, err
	}
	defer func() { _ = client.Close() }()
	return client.Call(method, params)
}

func (c testDaemonClient) CallInto(method string, params, out any) error {
	client, err := api.Dial(c.socketPath)
	if err != nil {
		return err
	}
	defer func() { _ = client.Close() }()
	return client.CallInto(method, params, out)
}

func (c testDaemonClient) Close() error { return nil }

func startTestDaemon(t *testing.T, home string) (*daemon.RunManager, testDaemonClient) {
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

	client := testDaemonClient{socketPath: api.SocketPath(home)}
	probe, err := api.Dial(client.socketPath)
	if err != nil {
		t.Fatalf("dial daemon socket: %v", err)
	}
	_ = probe.Close()
	return rm, client
}

func TestGateAdmitPushRPC_ValidBareRepoAdmitted(t *testing.T) {
	home := shortTempDir(t)
	_, client := startTestDaemon(t, home)

	barePath := gitgate.GatePath(home, "fixture/repo")
	if err := gitgate.InitBare(barePath); err != nil {
		t.Fatalf("InitBare: %v", err)
	}

	if _, err := client.Call("gate.admitPush", gateAdmitPushParams{GatePath: barePath}); err != nil {
		t.Fatalf("gate.admitPush: %v", err)
	}
}

func TestGateAdmitPushRPC_RejectsBareRepoOutsideMadeHome(t *testing.T) {
	home := shortTempDir(t)
	_, client := startTestDaemon(t, home)

	barePath := filepath.Join(shortTempDir(t), "unmanaged.git")
	if err := gitgate.InitBare(barePath); err != nil {
		t.Fatalf("InitBare: %v", err)
	}

	if _, err := client.Call("gate.admitPush", gateAdmitPushParams{GatePath: barePath}); err == nil {
		t.Fatal("gate.admitPush accepted a bare repository outside MADE_HOME")
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

	barePath := gitgate.GatePath(home, "fixture/repo")
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
