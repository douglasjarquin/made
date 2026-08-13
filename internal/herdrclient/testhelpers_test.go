package herdrclient

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

type fakeHerdrServer struct {
	listener net.Listener
	protocol int
	version  string

	mu       sync.Mutex
	payloads []map[string]any
	revision uint64
}

func newFakeHerdrServer(t *testing.T, protocol int) *fakeHerdrServer {
	t.Helper()
	// A short, non-test-name-derived temp dir keeps the socket path under the
	// platform's sun_path length limit (t.TempDir() nests a long per-test
	// directory that overflows it on macOS).
	dir, err := os.MkdirTemp("", "hc")
	if err != nil {
		t.Fatalf("newFakeHerdrServer: mkdtemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	path := filepath.Join(dir, "made.sock")
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatalf("newFakeHerdrServer: listen: %v", err)
	}
	s := &fakeHerdrServer{listener: ln, protocol: protocol, version: "9.9.9-fake"}
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
	s.mu.Lock()
	s.payloads = append(s.payloads, payload)
	s.mu.Unlock()

	id, _ := payload["id"].(string)
	method, _ := payload["method"].(string)

	var resp map[string]any
	switch method {
	case "ping":
		resp = map[string]any{
			"id": id,
			"result": map[string]any{
				"type":     "pong",
				"version":  s.version,
				"protocol": s.protocol,
			},
		}
	case "workspace.create":
		resp = map[string]any{
			"id": id,
			"result": map[string]any{
				"type": "workspace_created",
				"root_pane": map[string]any{
					"pane_id":      "pane-1",
					"workspace_id": "ws-1",
					"tab_id":       "tab-1",
				},
			},
		}
	case "pane.read":
		s.mu.Lock()
		s.revision++
		rev := s.revision
		s.mu.Unlock()
		resp = map[string]any{
			"id": id,
			"result": map[string]any{
				"type": "pane_read",
				"read": map[string]any{
					"text":     fmt.Sprintf("line %d\n", rev),
					"revision": rev,
				},
			},
		}
	case "pane.close":
		resp = map[string]any{
			"id":     id,
			"result": map[string]any{"type": "ok"},
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

func (s *fakeHerdrServer) recordedPayloads() []map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]map[string]any, len(s.payloads))
	copy(out, s.payloads)
	return out
}
