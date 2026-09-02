package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestConfigPathCommand_ReportsRootLayout(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".made.yaml"), []byte("version: 1\n"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	stdout, stderr, code := runCapture(t, []string{"config", "path", "--json", "--repo", dir})
	if code != 0 {
		t.Fatalf("expected exit 0, got %d, stderr:\n%s", code, stderr)
	}
	var report configPathReport
	if err := json.Unmarshal(stdout, &report); err != nil {
		t.Fatalf("unmarshal stdout %q: %v", stdout, err)
	}
	if report.Layout != "root" {
		t.Fatalf("Layout = %q, want %q", report.Layout, "root")
	}
	if report.Path != filepath.Join(dir, ".made.yaml") {
		t.Fatalf("Path = %q, want %q", report.Path, filepath.Join(dir, ".made.yaml"))
	}
}

func TestConfigPathCommand_ReportsAbsentWithoutMutation(t *testing.T) {
	dir := t.TempDir()

	stdout, stderr, code := runCapture(t, []string{"config", "path", "--json", "--repo", dir})
	if code != 0 {
		t.Fatalf("expected exit 0, got %d, stderr:\n%s", code, stderr)
	}
	var report configPathReport
	if err := json.Unmarshal(stdout, &report); err != nil {
		t.Fatalf("unmarshal stdout %q: %v", stdout, err)
	}
	if report.Layout != "absent" {
		t.Fatalf("Layout = %q, want %q", report.Layout, "absent")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected made config path to mutate nothing, found %d entries", len(entries))
	}
}

func TestConfigPathCommand_ReportsConflictWithNonZeroExit(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".made.yaml"), []byte("version: 1\n"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, ".made"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".made", "config.yaml"), []byte("version: 1\n"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	stdout, _, code := runCapture(t, []string{"config", "path", "--json", "--repo", dir})
	if code != 1 {
		t.Fatalf("expected exit 1 on conflict, got %d", code)
	}
	var report configPathReport
	if err := json.Unmarshal(stdout, &report); err != nil {
		t.Fatalf("unmarshal stdout %q: %v", stdout, err)
	}
	if report.Layout != "conflict" {
		t.Fatalf("Layout = %q, want %q", report.Layout, "conflict")
	}
	if report.Error == "" {
		t.Fatalf("expected a non-empty error message for a conflict")
	}
}

func TestConfigCheckCommand_ValidatesSelectedConfig(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".made.yaml"), []byte("agent: codex\n"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	stdout, _, code := runCapture(t, []string{"config", "check", "--json", "--repo", dir})
	if code != 1 {
		t.Fatalf("expected exit 1 for a config file missing version: 1, got %d", code)
	}
	var report configCheckReport
	if err := json.Unmarshal(stdout, &report); err != nil {
		t.Fatalf("unmarshal stdout %q: %v", stdout, err)
	}
	if report.Valid {
		t.Fatalf("expected Valid=false for a config file missing version: 1")
	}
}

func TestConfigCheckCommand_DoesNotStartAPipeline(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".made.yaml"), []byte("version: 1\nno_ci: true\n"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	stdout, stderr, code := runCapture(t, []string{"config", "check", "--json", "--repo", dir})
	if code != 0 {
		t.Fatalf("expected exit 0, got %d, stderr:\n%s", code, stderr)
	}
	var report configCheckReport
	if err := json.Unmarshal(stdout, &report); err != nil {
		t.Fatalf("unmarshal stdout %q: %v", stdout, err)
	}
	if !report.Valid {
		t.Fatalf("expected Valid=true, got report=%+v", report)
	}
}

func TestConfigMoveCommand_RootToDirectoryPreservesBytes(t *testing.T) {
	dir := t.TempDir()
	content := "version: 1\nno_ci: true\n"
	if err := os.WriteFile(filepath.Join(dir, ".made.yaml"), []byte(content), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	stdout, stderr, code := runCapture(t, []string{"config", "move", "--to", "directory", "--repo", dir})
	if code != 0 {
		t.Fatalf("expected exit 0, got %d, stdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
	if _, err := os.Stat(filepath.Join(dir, ".made.yaml")); !os.IsNotExist(err) {
		t.Fatalf("expected .made.yaml removed after move")
	}
	got, err := os.ReadFile(filepath.Join(dir, ".made", "config.yaml"))
	if err != nil {
		t.Fatalf("read moved config: %v", err)
	}
	if string(got) != content {
		t.Fatalf("moved content = %q, want exact bytes %q", got, content)
	}
}

func TestConfigMoveCommand_RefusesToOverwrite(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".made.yml"), []byte("version: 1\n"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".made.yaml"), []byte("version: 1\nno_ci: true\n"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	_, stderr, code := runCapture(t, []string{"config", "move", "--to", "root", "--repo", dir})
	if code == 0 {
		t.Fatalf("expected non-zero exit refusing an ambiguous move source, stderr:\n%s", stderr)
	}
}

func TestConfigMoveCommand_NeverCommits(t *testing.T) {
	dir := initRepoForPlanTest(t)
	if err := os.WriteFile(filepath.Join(dir, ".made.yaml"), []byte("version: 1\nno_ci: true\n"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	runGitForPlanTest(t, dir, "add", "-A")
	runGitForPlanTest(t, dir, "commit", "-q", "-m", "add config")
	headBefore := runGitForPlanTest(t, dir, "rev-parse", "HEAD")

	_, stderr, code := runCapture(t, []string{"config", "move", "--to", "directory", "--repo", dir})
	if code != 0 {
		t.Fatalf("expected exit 0, stderr:\n%s", stderr)
	}

	headAfter := runGitForPlanTest(t, dir, "rev-parse", "HEAD")
	if headBefore != headAfter {
		t.Fatalf("made config move created a commit: HEAD moved from %s to %s", headBefore, headAfter)
	}
	status := runGitForPlanTest(t, dir, "status", "--porcelain")
	if status == "" {
		t.Fatalf("expected the move to leave working-tree changes unstaged, found none")
	}
}
