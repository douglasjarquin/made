package verify_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/douglasjarquin/made/internal/verify"
)

func TestRequest_PublishAndLoadRoundTrip(t *testing.T) {
	dir, _, _ := newTestRepo(t, ".made.yaml", testConfigReviewRequired)
	rc, err := verify.ResolveContext(context.Background(), dir, "origin/main")
	if err != nil {
		t.Fatalf("ResolveContext: %v", err)
	}
	req, err := verify.BuildRequest(rc, "run-1", "inv-1", "cursor", "claude-opus", nil)
	if err != nil {
		t.Fatalf("BuildRequest: %v", err)
	}

	path := filepath.Join(t.TempDir(), "nested", "request.json")
	if err := verify.PublishRequest(path, req); err != nil {
		t.Fatalf("PublishRequest: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat published request: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("request file mode = %v, want 0600", info.Mode().Perm())
	}

	loaded, err := verify.LoadRequest(path)
	if err != nil {
		t.Fatalf("LoadRequest: %v", err)
	}
	if loaded.ContractHash != req.ContractHash || loaded.Executor != "cursor" || loaded.RequestedModel != "claude-opus" {
		t.Errorf("loaded request mismatch: %+v", loaded)
	}
}

func TestRequest_LoadRejectsUnknownFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "request.json")
	if err := os.WriteFile(path, []byte(`{"schema_version":1,"unexpected_field":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := verify.LoadRequest(path); err == nil {
		t.Fatal("expected LoadRequest to reject an unknown field")
	}
}

func TestRequest_LoadRejectsWrongSchemaVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "request.json")
	if err := os.WriteFile(path, []byte(`{"schema_version":99}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := verify.LoadRequest(path); err == nil {
		t.Fatal("expected LoadRequest to reject an unsupported schema_version")
	}
}

func TestRequest_LoadRejectsMultipleDocuments(t *testing.T) {
	path := filepath.Join(t.TempDir(), "request.json")
	if err := os.WriteFile(path, []byte(`{"schema_version":1}{"schema_version":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := verify.LoadRequest(path); err == nil {
		t.Fatal("expected LoadRequest to reject more than one JSON document")
	}
}

func TestRequest_LoadRejectsOversizedFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "request.json")
	huge := strings.Repeat("a", 2<<20)
	if err := os.WriteFile(path, []byte(`{"schema_version":1,"pad":"`+huge+`"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := verify.LoadRequest(path); err == nil {
		t.Fatal("expected LoadRequest to reject an oversized file")
	}
}

func TestReadTaskFile_BoundsAndEmbeds(t *testing.T) {
	path := filepath.Join(t.TempDir(), "task.md")
	if err := os.WriteFile(path, []byte("do the thing\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	task, err := verify.ReadTaskFile(path)
	if err != nil {
		t.Fatalf("ReadTaskFile: %v", err)
	}
	if task == nil || task.Content != "do the thing\n" || task.Bytes != len("do the thing\n") {
		t.Fatalf("task = %+v, want embedded content", task)
	}

	empty, err := verify.ReadTaskFile("")
	if err != nil || empty != nil {
		t.Fatalf("ReadTaskFile(\"\") = %+v, %v; want nil, nil", empty, err)
	}
}

func TestReadTaskFile_RejectsOversized(t *testing.T) {
	path := filepath.Join(t.TempDir(), "task.md")
	if err := os.WriteFile(path, make([]byte, 128*1024), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := verify.ReadTaskFile(path); err == nil {
		t.Fatal("expected ReadTaskFile to reject an oversized task file")
	}
}
