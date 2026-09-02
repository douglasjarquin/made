package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

const laneConfigFixture = `
version: 1
validation:
  lanes:
    go:
      paths:
        - "**/*.go"
      required_before_push: true
`

func TestPlanCommand_ResolvesConfigFromRootYamlLayout(t *testing.T) {
	dir := initRepoForPlanTest(t)
	if err := os.WriteFile(filepath.Join(dir, ".made.yaml"), []byte(laneConfigFixture), 0o644); err != nil {
		t.Fatalf("write .made.yaml: %v", err)
	}
	runGitForPlanTest(t, dir, "add", "-A")
	runGitForPlanTest(t, dir, "commit", "-q", "-m", "add .made.yaml")
	runGitForPlanTest(t, dir, "checkout", "-q", "-b", "feature")
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("write main.go: %v", err)
	}
	runGitForPlanTest(t, dir, "add", "-A")
	runGitForPlanTest(t, dir, "commit", "-q", "-m", "add main.go")

	stdout, stderr, code := runCapture(t, []string{"plan", "--json", "--base", "main", "--repo", dir})
	if code != 0 {
		t.Fatalf("expected exit 0, got %d, stderr:\n%s", code, stderr)
	}
	var report struct {
		Lanes []struct {
			Name   string `json:"name"`
			Action string `json:"action"`
		} `json:"lanes"`
	}
	if err := json.Unmarshal(stdout, &report); err != nil {
		t.Fatalf("decode plan JSON: %v\noutput:\n%s", err, stdout)
	}
	for _, l := range report.Lanes {
		if l.Name == "go" {
			if l.Action != "run" {
				t.Fatalf("expected go lane to run per .made.yaml's configured lane, got %+v", l)
			}
			return
		}
	}
	t.Fatalf("expected .made.yaml's configured \"go\" lane in the plan, got %+v", report.Lanes)
}

func TestPlanCommand_ResolvesConfigFromDirectoryLayout(t *testing.T) {
	dir := initRepoForPlanTest(t)
	if err := os.MkdirAll(filepath.Join(dir, ".made"), 0o755); err != nil {
		t.Fatalf("mkdir .made: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".made", "config.yaml"), []byte(laneConfigFixture), 0o644); err != nil {
		t.Fatalf("write .made/config.yaml: %v", err)
	}
	runGitForPlanTest(t, dir, "add", "-A")
	runGitForPlanTest(t, dir, "commit", "-q", "-m", "add .made/config.yaml")
	runGitForPlanTest(t, dir, "checkout", "-q", "-b", "feature")
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("write main.go: %v", err)
	}
	runGitForPlanTest(t, dir, "add", "-A")
	runGitForPlanTest(t, dir, "commit", "-q", "-m", "add main.go")

	stdout, stderr, code := runCapture(t, []string{"plan", "--json", "--base", "main", "--repo", dir})
	if code != 0 {
		t.Fatalf("expected exit 0, got %d, stderr:\n%s", code, stderr)
	}
	var report struct {
		Lanes []struct {
			Name   string `json:"name"`
			Action string `json:"action"`
		} `json:"lanes"`
	}
	if err := json.Unmarshal(stdout, &report); err != nil {
		t.Fatalf("decode plan JSON: %v\noutput:\n%s", err, stdout)
	}
	for _, l := range report.Lanes {
		if l.Name == "go" {
			if l.Action != "run" {
				t.Fatalf("expected go lane to run per .made/config.yaml's configured lane, got %+v", l)
			}
			return
		}
	}
	t.Fatalf("expected .made/config.yaml's configured \"go\" lane in the plan, got %+v", report.Lanes)
}

func TestPlanCommand_FailsClosedOnConflictingLayouts(t *testing.T) {
	dir := initRepoForPlanTest(t)
	if err := os.WriteFile(filepath.Join(dir, ".made.yaml"), []byte("version: 1\n"), 0o644); err != nil {
		t.Fatalf("write .made.yaml: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, ".made"), 0o755); err != nil {
		t.Fatalf("mkdir .made: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".made", "config.yaml"), []byte("version: 1\n"), 0o644); err != nil {
		t.Fatalf("write .made/config.yaml: %v", err)
	}
	runGitForPlanTest(t, dir, "add", "-A")
	runGitForPlanTest(t, dir, "commit", "-q", "-m", "add conflicting configs")

	_, stderr, code := runCapture(t, []string{"plan", "--json", "--base", "main", "--repo", dir})
	if code == 0 {
		t.Fatalf("expected non-zero exit on conflicting config layouts, stderr:\n%s", stderr)
	}
}
