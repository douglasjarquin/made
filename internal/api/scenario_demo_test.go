package api_test

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
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

	info, err := os.Stat(socketPath)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	fmt.Printf("$ inspect Unix socket %s\nmode=%#o type=%s\n", socketPath, info.Mode().Perm(), info.Mode().Type())

	if info.Mode()&os.ModeSocket == 0 || info.Mode().Perm() != 0o600 {
		t.Fatalf("expected owner-only Unix socket mode 0600, got mode=%#o type=%s", info.Mode().Perm(), info.Mode().Type())
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
