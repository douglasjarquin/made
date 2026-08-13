package herdrclient

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultSocketPath(t *testing.T) {
	got := defaultSocketPath("/home/doug", "made")
	want := filepath.Join("/home/doug", ".config", "herdr", "sockets", "made.sock")
	if got != want {
		t.Fatalf("defaultSocketPath() = %q, want %q", got, want)
	}
}

func TestSocketPathEnvOverride(t *testing.T) {
	t.Setenv("HERDR_SOCKET_PATH", "/tmp/custom-herdr.sock")
	if got := SocketPath(); got != "/tmp/custom-herdr.sock" {
		t.Fatalf("SocketPath() = %q, want env override", got)
	}
}

func TestSocketPathDefaultsWhenEnvUnset(t *testing.T) {
	t.Setenv("HERDR_SOCKET_PATH", "")
	_ = os.Unsetenv("HERDR_SOCKET_PATH")

	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir: %v", err)
	}
	want := defaultSocketPath(home, SessionName)
	if got := SocketPath(); got != want {
		t.Fatalf("SocketPath() = %q, want %q", got, want)
	}
}
