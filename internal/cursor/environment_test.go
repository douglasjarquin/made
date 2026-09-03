package cursor_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/douglasjarquin/made/internal/config"
	"github.com/douglasjarquin/made/internal/cursor"
)

type cursorCloudEnv struct {
	Install string `json:"install"`
	Build   struct {
		Dockerfile string `json:"dockerfile"`
		Context    string `json:"context"`
	} `json:"build"`
}

func TestCursorCloudEnvironment_IsCommittedAndRunnable(t *testing.T) {
	root := cursorRepoRoot(t)
	envPath := filepath.Join(root, ".cursor", "environment.json")
	data, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatalf("expected committed .cursor/environment.json for Cursor Cloud: %v", err)
	}

	var env cursorCloudEnv
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&env); err != nil {
		t.Fatalf("environment.json must be strict JSON matching Cursor's schema fields: %v", err)
	}
	if env.Build.Dockerfile != "../docker/dev/Dockerfile" {
		t.Fatalf("dockerfile must be ../docker/dev/Dockerfile, got %q", env.Build.Dockerfile)
	}
	if env.Build.Context != ".." {
		t.Fatalf("context must be the repository root (..), got %q", env.Build.Context)
	}
	if !strings.Contains(env.Install, "scripts/install-cursor-cloud.sh") {
		t.Fatalf("install must run scripts/install-cursor-cloud.sh, got %q", env.Install)
	}
	if !strings.Contains(env.Install, "go mod download") {
		t.Fatalf("install must warm the module cache, got %q", env.Install)
	}

	dockerfilePath := filepath.Join(root, ".cursor", filepath.FromSlash(env.Build.Dockerfile))
	dockerfile, err := os.ReadFile(dockerfilePath)
	if err != nil {
		t.Fatalf("Dockerfile %s (relative to .cursor/) must exist: %v", env.Build.Dockerfile, err)
	}
	body := string(dockerfile)
	goVersion := goModVersion(t, root)
	if !strings.Contains(body, "GO_VERSION="+goVersion) {
		t.Fatalf("Dockerfile must pin GO_VERSION=%s from go.mod, got:\n%s", goVersion, body)
	}
	for _, need := range []string{"bubblewrap", "git", "gcc", "GOTOOLCHAIN=local", "/home/ubuntu/.local/bin"} {
		if !strings.Contains(body, need) {
			t.Fatalf("Dockerfile must install/configure %q, got:\n%s", need, body)
		}
	}
	if strings.Contains(body, "COPY .") || strings.Contains(body, "COPY ..") {
		t.Fatal("Dockerfile must not COPY the repository; Cursor checks out the requested commit itself")
	}
}

func TestManagedFiles_DoesNotOwnCloudEnvironment(t *testing.T) {
	files, err := cursor.ManagedFiles(config.Config{
		Review: config.Review{
			Executors: config.ReviewExecutors{
				Cursor: config.CursorExecutor{Model: "gpt-5"},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	owned := make([]string, 0, len(files))
	for _, f := range files {
		owned = append(owned, f.RelPath)
		if strings.Contains(f.RelPath, "environment.json") || filepath.Base(f.RelPath) == "Dockerfile" {
			t.Fatalf("made cursor must not own %s; Cloud environment files are hand-written", f.RelPath)
		}
	}
	if !slices.Contains(owned, cursor.SkillRelPath) || !slices.Contains(owned, cursor.ReviewerRelPath) {
		t.Fatalf("expected skill and reviewer to stay managed, got %v", owned)
	}
}

func cursorRepoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func goModVersion(t *testing.T, root string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	for line := range strings.SplitSeq(string(data), "\n") {
		line = strings.TrimSpace(line)
		if version, ok := strings.CutPrefix(line, "go "); ok {
			return strings.TrimSpace(version)
		}
	}
	t.Fatal("go.mod is missing a go version line")
	return ""
}
