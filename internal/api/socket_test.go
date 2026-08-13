package api_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/douglasjarquin/made/internal/api"
)

func TestServer_SocketHasOwnerOnlyPermissions(t *testing.T) {
	socketPath := filepath.Join(tempSocketDir(t), "daemon.sock")
	srv := api.NewServer(socketPath)
	if err := srv.Listen(); err != nil {
		t.Fatalf("Listen: %v", err)
	}
	t.Cleanup(func() { _ = srv.Close() })

	info, err := os.Stat(socketPath)
	if err != nil {
		t.Fatalf("stat socket: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Fatalf("expected socket permissions 0600, got %o", mode)
	}
}
