package api_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/douglasjarquin/made/internal/api"
)

// tempSocketDir avoids t.TempDir(): it nests fixtures under a per-test
// path built from the test's full name, which routinely blows past the
// ~104-byte sockaddr_un.sun_path limit on macOS and makes net.Listen fail
// with "invalid argument" for reasons unrelated to the code under test.
func tempSocketDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "made-api")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

func startTestServer(t *testing.T) (*api.Server, *api.Client) {
	t.Helper()

	socketPath := filepath.Join(tempSocketDir(t), "daemon.sock")
	srv := api.NewServer(socketPath)
	if err := srv.Listen(); err != nil {
		t.Fatalf("Listen: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- srv.Serve(ctx) }()

	t.Cleanup(func() {
		cancel()
		<-done
		_ = srv.Close()
	})

	client, err := api.Dial(socketPath)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })

	return srv, client
}
