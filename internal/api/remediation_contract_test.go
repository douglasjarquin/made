package api_test

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/douglasjarquin/made/internal/api"
)

func TestServer_RefusesExistingNonSocketPaths(t *testing.T) {
	cases := []struct {
		name string
		make func(t *testing.T, path string)
		keep func(t *testing.T, path string)
	}{
		{
			name: "regular file",
			make: func(t *testing.T, path string) {
				t.Helper()
				if err := os.WriteFile(path, []byte("owner data"), 0o600); err != nil {
					t.Fatalf("write regular file: %v", err)
				}
			},
			keep: func(t *testing.T, path string) {
				t.Helper()
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatalf("read preserved regular file: %v", err)
				}
				if string(data) != "owner data" {
					t.Fatalf("regular file contents changed to %q", data)
				}
			},
		},
		{
			name: "symlink",
			make: func(t *testing.T, path string) {
				t.Helper()
				target := filepath.Join(filepath.Dir(path), "socket-target")
				if err := os.WriteFile(target, []byte("target"), 0o600); err != nil {
					t.Fatalf("write symlink target: %v", err)
				}
				if err := os.Symlink(target, path); err != nil {
					t.Fatalf("create symlink: %v", err)
				}
			},
			keep: func(t *testing.T, path string) {
				t.Helper()
				info, err := os.Lstat(path)
				if err != nil {
					t.Fatalf("lstat preserved symlink: %v", err)
				}
				if info.Mode()&os.ModeSymlink == 0 {
					t.Fatalf("path mode %v is no longer a symlink", info.Mode())
				}
			},
		},
		{
			name: "directory",
			make: func(t *testing.T, path string) {
				t.Helper()
				if err := os.Mkdir(path, 0o700); err != nil {
					t.Fatalf("make directory: %v", err)
				}
				if err := os.WriteFile(filepath.Join(path, "owner-data"), []byte("keep"), 0o600); err != nil {
					t.Fatalf("write directory marker: %v", err)
				}
			},
			keep: func(t *testing.T, path string) {
				t.Helper()
				info, err := os.Stat(path)
				if err != nil {
					t.Fatalf("stat preserved directory: %v", err)
				}
				if !info.IsDir() {
					t.Fatalf("path mode %v is no longer a directory", info.Mode())
				}
				if _, err := os.Stat(filepath.Join(path, "owner-data")); err != nil {
					t.Fatalf("directory marker was removed: %v", err)
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "daemon.sock")
			tc.make(t, path)

			srv := api.NewServer(path)
			err := srv.Listen()
			if err == nil {
				_ = srv.Close()
				t.Fatal("Listen accepted an existing non-socket path")
			}
			tc.keep(t, path)
		})
	}
}

func TestServerRejectsOversizedRequestLine(t *testing.T) {
	path := filepath.Join(tempSocketDir(t), "daemon.sock")
	server := api.NewServer(path)
	if err := server.Listen(); err != nil {
		t.Fatalf("Listen: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	defer func() { _ = server.Close() }()
	go func() { _ = server.Serve(ctx) }()

	conn, err := net.DialTimeout("unix", path, time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()
	request := "{\"protocol\":1,\"id\":\"oversized\",\"method\":\"ping\",\"params\":\"" + strings.Repeat("x", 2<<20) + "\"}\n"
	if _, err := conn.Write([]byte(request)); err != nil {
		return
	}
	if err := conn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatalf("set read deadline: %v", err)
	}
	var response api.Response
	if err := json.NewDecoder(conn).Decode(&response); err == nil {
		t.Fatal("server accepted an oversized request line")
	}
}

func TestServer_DuplicateListenPreservesOriginalOwner(t *testing.T) {
	path := filepath.Join(tempSocketDir(t), "daemon.sock")
	first := api.NewServer(path)
	if err := first.Listen(); err != nil {
		t.Fatalf("first Listen: %v", err)
	}
	defer func() { _ = first.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	go func() { _ = first.Serve(ctx) }()

	if err := waitForPing(path); err != nil {
		t.Fatalf("first server did not answer ping: %v", err)
	}

	second := api.NewServer(path)
	if err := second.Listen(); err == nil {
		_ = second.Close()
		t.Fatal("duplicate Listen unexpectedly acquired the original socket path")
	}
	if err := waitForPing(path); err != nil {
		t.Fatalf("original server became unreachable after duplicate Listen: %v", err)
	}
}

func TestPrepareSocket_PreservesLiveOwnerSocket(t *testing.T) {
	path := filepath.Join(tempSocketDir(t), "daemon.sock")
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatalf("listen live owner socket: %v", err)
	}
	defer func() { _ = listener.Close() }()

	if err := api.PrepareSocket(path); err == nil {
		t.Fatal("PrepareSocket replaced a live owner socket")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("live owner socket was not preserved: %v", err)
	}
	conn, err := net.DialTimeout("unix", path, 100*time.Millisecond)
	if err != nil {
		t.Fatalf("preserved live owner socket is not reachable: %v", err)
	}
	_ = conn.Close()
}

func TestServer_CloseDoesNotRemoveSuccessorSocket(t *testing.T) {
	path := filepath.Join(tempSocketDir(t), "daemon.sock")
	first := api.NewServer(path)
	if err := first.Listen(); err != nil {
		t.Fatalf("first Listen: %v", err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatalf("remove first socket for handoff fixture: %v", err)
	}
	second := api.NewServer(path)
	if err := second.Listen(); err != nil {
		t.Fatalf("successor Listen: %v", err)
	}
	defer func() { _ = second.Close() }()
	secondCtx, secondCancel := context.WithTimeout(context.Background(), time.Second)
	defer secondCancel()
	go func() { _ = second.Serve(secondCtx) }()

	if err := first.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := waitForPing(path); err != nil {
		t.Fatalf("successor socket was removed by first Close: %v", err)
	}
}

func waitForPing(path string) error {
	conn, err := net.DialTimeout("unix", path, 100*time.Millisecond)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()
	if err := conn.SetDeadline(time.Now().Add(100 * time.Millisecond)); err != nil {
		return err
	}
	if err := json.NewEncoder(conn).Encode(api.Request{Protocol: api.Version, ID: "ping", Method: "ping"}); err != nil {
		return err
	}
	var response api.Response
	if err := json.NewDecoder(bufio.NewReader(conn)).Decode(&response); err != nil {
		return err
	}
	if response.Error != nil {
		return errors.New(response.Error.Error())
	}
	return nil
}
