package orchestrator

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/douglasjarquin/made/internal/gitgate"
)

func initGateFixture(t *testing.T, barePath string) error {
	t.Helper()
	if err := gitgate.InitBare(barePath); err != nil {
		return err
	}
	runGit(t, barePath, "remote", "add", "origin", barePath)
	return nil
}

func TestSetupResolvesTrustedConfigFromRootYamlLayout(t *testing.T) {
	dir := t.TempDir()
	barePath := filepath.Join(dir, "gate.git")
	if err := initGateFixture(t, barePath); err != nil {
		t.Fatalf("initGateFixture: %v", err)
	}

	src := filepath.Join(dir, "src")
	initSourceRepo(t, src)
	writeFile(t, src, ".made.yaml", "version: 1\nno_ci: true\ncommands:\n  test: \"go test ./...\"\n")
	commit(t, src, "add .made.yaml")
	sha := pushBranch(t, src, barePath, "main")

	worktreesDir := filepath.Join(dir, "worktrees")
	rc, err := Setup(context.Background(), barePath, "main", worktreesDir, "run-root-yaml", sha)
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}
	defer rc.Cleanup(context.Background())

	if !rc.Config.NoCI {
		t.Fatalf("expected NoCI true from .made.yaml trusted config, got %+v", rc.Config)
	}
}

func TestSetupResolvesTrustedConfigFromDirectoryLayout(t *testing.T) {
	dir := t.TempDir()
	barePath := filepath.Join(dir, "gate.git")
	if err := initGateFixture(t, barePath); err != nil {
		t.Fatalf("initGateFixture: %v", err)
	}

	src := filepath.Join(dir, "src")
	initSourceRepo(t, src)
	writeFile(t, src, ".made/config.yaml", "version: 1\nno_ci: true\ncommands:\n  test: \"go test ./...\"\n")
	commit(t, src, "add .made/config.yaml")
	sha := pushBranch(t, src, barePath, "main")

	worktreesDir := filepath.Join(dir, "worktrees")
	rc, err := Setup(context.Background(), barePath, "main", worktreesDir, "run-dir-yaml", sha)
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}
	defer rc.Cleanup(context.Background())

	if !rc.Config.NoCI {
		t.Fatalf("expected NoCI true from .made/config.yaml trusted config, got %+v", rc.Config)
	}
}

func TestSetupFailsClosedWhenTrustedConfigConflicts(t *testing.T) {
	dir := t.TempDir()
	barePath := filepath.Join(dir, "gate.git")
	if err := initGateFixture(t, barePath); err != nil {
		t.Fatalf("initGateFixture: %v", err)
	}

	src := filepath.Join(dir, "src")
	initSourceRepo(t, src)
	writeFile(t, src, ".made.yaml", "version: 1\nno_ci: true\n")
	writeFile(t, src, ".made/config.yaml", "version: 1\nno_ci: true\n")
	commit(t, src, "add conflicting configs")
	sha := pushBranch(t, src, barePath, "main")

	worktreesDir := filepath.Join(dir, "worktrees")
	rc, err := Setup(context.Background(), barePath, "main", worktreesDir, "run-trusted-conflict", sha)
	if err == nil {
		defer rc.Cleanup(context.Background())
		t.Fatalf("expected Setup to fail closed on conflicting trusted config, got rc=%+v", rc)
	}
	if rc != nil {
		t.Fatalf("expected nil RunContext on trusted config conflict, got %+v", rc)
	}
}

func TestSetupFailsClosedWhenCandidateConfigConflicts(t *testing.T) {
	dir := t.TempDir()
	barePath := filepath.Join(dir, "gate.git")
	if err := initGateFixture(t, barePath); err != nil {
		t.Fatalf("initGateFixture: %v", err)
	}

	src := filepath.Join(dir, "src")
	initSourceRepo(t, src)
	pushBranch(t, src, barePath, "main")

	writeFile(t, src, ".made.yaml", "version: 1\nno_ci: true\n")
	writeFile(t, src, ".made/config.yaml", "version: 1\nno_ci: true\n")
	commit(t, src, "add conflicting configs on feature branch")
	featureSHA := pushBranch(t, src, barePath, "feature")

	worktreesDir := filepath.Join(dir, "worktrees")
	rc, err := Setup(context.Background(), barePath, "main", worktreesDir, "run-candidate-conflict", featureSHA)
	if err == nil {
		defer rc.Cleanup(context.Background())
		t.Fatalf("expected Setup to fail closed on conflicting candidate config, got rc=%+v", rc)
	}
	if rc != nil {
		t.Fatalf("expected nil RunContext on candidate config conflict, got %+v", rc)
	}
	entries, rdErr := os.ReadDir(worktreesDir)
	if rdErr != nil && !os.IsNotExist(rdErr) {
		t.Fatalf("read worktrees dir: %v", rdErr)
	}
	if len(entries) != 0 {
		t.Fatalf("expected zero leftover worktree dirs after candidate config conflict, found %d: %v", len(entries), entries)
	}
}

func TestSetupAllowsDifferingLayoutsBetweenTrustedAndCandidate(t *testing.T) {
	dir := t.TempDir()
	barePath := filepath.Join(dir, "gate.git")
	if err := initGateFixture(t, barePath); err != nil {
		t.Fatalf("initGateFixture: %v", err)
	}

	src := filepath.Join(dir, "src")
	initSourceRepo(t, src)
	writeFile(t, src, ".made.yaml", "version: 1\nno_ci: true\nallow_repo_commands: true\n")
	commit(t, src, "add root-layout trusted config")
	pushBranch(t, src, barePath, "main")

	writeFile(t, src, "unrelated.txt", "feature work\n")
	if err := os.Remove(filepath.Join(src, ".made.yaml")); err != nil {
		t.Fatalf("remove .made.yaml on feature branch: %v", err)
	}
	writeFile(t, src, ".made/config.yaml", "version: 1\ncommands:\n  test: \"go test ./...\"\n")
	commit(t, src, "candidate switches to directory layout")
	featureSHA := pushBranch(t, src, barePath, "feature")

	worktreesDir := filepath.Join(dir, "worktrees")
	rc, err := Setup(context.Background(), barePath, "main", worktreesDir, "run-transition", featureSHA)
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}
	defer rc.Cleanup(context.Background())

	if rc.Config.Commands.Test != "go test ./..." {
		t.Fatalf("expected candidate's directory-layout test command honored under allow_repo_commands, got %q", rc.Config.Commands.Test)
	}
}
