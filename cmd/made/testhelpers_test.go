package main

import (
	"os"
	"testing"
)

// shortTempDir mirrors internal/api's own tempSocketDir helper: t.TempDir()
// nests fixtures under a path built from the test's full name, which
// routinely blows past the ~104-byte sockaddr_un.sun_path limit on macOS and
// makes net.Listen fail with "invalid argument" for reasons unrelated to the
// code under test.
func shortTempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "made-cli")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}
