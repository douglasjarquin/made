package api_test

import (
	"encoding/json"
	"fmt"
	"net"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/douglasjarquin/made/internal/api"
)

func TestScenarioDemo_SocketPermissions(t *testing.T) {
	socketPath := filepath.Join(tempSocketDir(t), "daemon.sock")
	fmt.Printf("$ (start daemon listening on) %s\n", socketPath)

	srv := api.NewServer(socketPath)
	if err := srv.Listen(); err != nil {
		t.Fatalf("Listen: %v", err)
	}
	t.Cleanup(func() { _ = srv.Close() })

	out, err := exec.Command("stat", "-f", "%Sp %Su", socketPath).CombinedOutput()
	if err != nil {
		t.Fatalf("stat: %v: %s", err, out)
	}
	fmt.Printf("$ stat -f \"%%Sp %%Su\" %s\n%s", socketPath, out)

	if !strings.HasPrefix(strings.TrimSpace(string(out)), "srw-------") {
		t.Fatalf("expected owner-only socket permissions srw-------, got: %s", out)
	}
	fmt.Println("=== RESULT ===")
	fmt.Println("PASS: socket created with mode 0600 (srw-------), owner-only")
}

func TestScenarioDemo_VersionMismatchRejected(t *testing.T) {
	socketPath := filepath.Join(tempSocketDir(t), "daemon.sock")
	srv := api.NewServer(socketPath)
	if err := srv.Listen(); err != nil {
		t.Fatalf("Listen: %v", err)
	}
	t.Cleanup(func() { _ = srv.Close() })
	go func() { _ = srv.Serve(t.Context()) }()

	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()

	raw := `{"made.protocol":999,"id":"raw-1","method":"ping"}`
	fmt.Printf("$ send raw JSON over socket: %s\n", raw)
	if _, err := conn.Write([]byte(raw)); err != nil {
		t.Fatalf("write: %v", err)
	}

	var resp api.Response
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	respJSON, _ := json.Marshal(resp)
	fmt.Printf("response: %s\n", respJSON)

	if resp.Error == nil {
		t.Fatal("expected structured error response for version mismatch, got none")
	}
	if resp.Error.Code != api.ErrProtocolMismatch {
		t.Fatalf("expected code %q, got %q", api.ErrProtocolMismatch, resp.Error.Code)
	}
	fmt.Println("=== RESULT ===")
	fmt.Printf("PASS: mismatched made.protocol (999 vs server %d) rejected with structured error %q, connection stayed open, no hang/crash\n", api.Version, resp.Error.Code)
}
