package pipeline_test

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/douglasjarquin/made/internal/exec"
	"github.com/douglasjarquin/made/internal/herdrclient"
	"github.com/douglasjarquin/made/internal/pipeline"
)

type fakeHerdrServer struct {
	listener net.Listener

	mu    sync.Mutex
	calls []string
}

func newFakeHerdrServer(t *testing.T) *fakeHerdrServer {
	t.Helper()

	dir, err := os.MkdirTemp("", "hv")
	if err != nil {
		t.Fatalf("newFakeHerdrServer: mkdtemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	path := filepath.Join(dir, "made.sock")
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatalf("newFakeHerdrServer: listen: %v", err)
	}

	s := &fakeHerdrServer{listener: ln}
	go s.serve()
	t.Cleanup(func() { _ = ln.Close() })
	return s
}

func (s *fakeHerdrServer) socketPath() string {
	return s.listener.Addr().String()
}

func (s *fakeHerdrServer) serve() {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			return
		}
		go s.handle(conn)
	}
}

func (s *fakeHerdrServer) handle(conn net.Conn) {
	defer func() { _ = conn.Close() }()

	reader := bufio.NewReader(conn)
	line, err := reader.ReadBytes('\n')
	if err != nil && len(line) == 0 {
		return
	}

	var payload map[string]any
	if err := json.Unmarshal(line, &payload); err != nil {
		return
	}

	id, _ := payload["id"].(string)
	method, _ := payload["method"].(string)

	s.mu.Lock()
	s.calls = append(s.calls, method)
	s.mu.Unlock()

	var resp map[string]any
	switch method {
	case "ping":
		resp = map[string]any{
			"id": id,
			"result": map[string]any{
				"version":  "9.9.9-fake",
				"protocol": herdrclient.RequiredProtocolVersion,
			},
		}
	case "workspace.create":
		resp = map[string]any{
			"id": id,
			"result": map[string]any{
				"root_pane": map[string]any{
					"pane_id":      "pane-1",
					"workspace_id": "ws-1",
					"tab_id":       "tab-1",
				},
			},
		}
	case "pane.close":
		resp = map[string]any{
			"id":     id,
			"result": map[string]any{},
		}
	default:
		resp = map[string]any{
			"id":    id,
			"error": map[string]any{"code": "unknown_method", "message": method},
		}
	}

	body, err := json.Marshal(resp)
	if err != nil {
		return
	}
	_, _ = conn.Write(append(body, '\n'))
}

func (s *fakeHerdrServer) callCount(method string) int {
	s.mu.Lock()
	defer s.mu.Unlock()

	n := 0
	for _, m := range s.calls {
		if m == method {
			n++
		}
	}
	return n
}

func unreachableSocketPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "no-herdr-here.sock")
}

func simulateGateRun(t *testing.T, runID string, shellCmd string) string {
	t.Helper()

	ctx := context.Background()
	v := pipeline.Open(ctx, runID)
	defer v.Close(ctx)

	res, err := exec.Run(ctx, exec.Command{Name: "sh", Args: []string{"-c", shellCmd}})
	if err != nil {
		t.Fatalf("exec.Run: %v", err)
	}
	if res.ExitCode == 0 {
		return "pass"
	}
	return "fail"
}

func TestOpen_FailOpenEquivalence(t *testing.T) {
	srv := newFakeHerdrServer(t)

	scenarios := []struct {
		name     string
		shellCmd string
	}{
		{name: "passing stage", shellCmd: "exit 0"},
		{name: "failing stage", shellCmd: "exit 1"},
	}

	for _, sc := range scenarios {
		t.Run(sc.name, func(t *testing.T) {
			t.Setenv("HERDR_SOCKET_PATH", srv.socketPath())
			withHerdr := simulateGateRun(t, fmt.Sprintf("run-%s-with-herdr", t.Name()), sc.shellCmd)

			t.Setenv("HERDR_SOCKET_PATH", unreachableSocketPath(t))
			withoutHerdr := simulateGateRun(t, fmt.Sprintf("run-%s-without-herdr", t.Name()), sc.shellCmd)

			if withHerdr != withoutHerdr {
				t.Fatalf("outcome depended on herdr availability: with herdr = %q, without herdr = %q", withHerdr, withoutHerdr)
			}
		})
	}
}

func TestOpen_PaneLifecycle(t *testing.T) {
	srv := newFakeHerdrServer(t)
	t.Setenv("HERDR_SOCKET_PATH", srv.socketPath())

	ctx := context.Background()
	v := pipeline.Open(ctx, "run-lifecycle")

	if got := srv.callCount("workspace.create"); got != 1 {
		t.Fatalf("workspace.create calls = %d, want 1 (pane should be opened during the run)", got)
	}
	if got := srv.callCount("pane.close"); got != 0 {
		t.Fatalf("pane.close calls = %d, want 0 before Close is called", got)
	}

	v.Close(ctx)

	if got := srv.callCount("pane.close"); got != 1 {
		t.Fatalf("pane.close calls = %d, want 1 (pane should be closed after the run)", got)
	}
}

func TestOpen_UnavailableHerdrReturnsUsableNoOpVisibility(t *testing.T) {
	t.Setenv("HERDR_SOCKET_PATH", unreachableSocketPath(t))

	ctx := context.Background()
	v := pipeline.Open(ctx, "run-no-herdr")
	if v == nil {
		t.Fatal("Open returned nil, want a usable no-op Visibility")
	}
	v.Close(ctx)
}
