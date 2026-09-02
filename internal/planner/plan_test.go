package planner_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/douglasjarquin/made/internal/config"
	"github.com/douglasjarquin/made/internal/planner"
)

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=planner-test", "GIT_AUTHOR_EMAIL=planner-test@example.com",
		"GIT_COMMITTER_NAME=planner-test", "GIT_COMMITTER_EMAIL=planner-test@example.com",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v in %s failed: %v: %s", args, dir, err, out)
	}
	return string(out)
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	full := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir for %s: %v", name, err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

func initRepoWithBase(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	runGit(t, dir, "init", "-q", "-b", "main")
	writeFile(t, dir, "README.md", "hello\n")
	runGit(t, dir, "add", "-A")
	runGit(t, dir, "commit", "-q", "-m", "base commit")
	return dir
}

func TestBuildPlan_IdenticalRerunHasNoChangedPathsAndSkipsDefaultLane(t *testing.T) {
	dir := initRepoWithBase(t)

	plan, err := planner.BuildPlan(context.Background(), dir, "main", "HEAD", config.Config{})
	if err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}

	if len(plan.ChangedPaths) != 0 {
		t.Fatalf("expected no changed paths for identical rerun, got %v", plan.ChangedPaths)
	}
	test := stageDecision(t, plan, "test")
	if test.Action != "skip" {
		t.Fatalf("expected test stage to skip on identical rerun, got %+v", test)
	}
	if test.Reason == "" {
		t.Fatal("expected a concrete skip reason, got empty string")
	}
}

func TestBuildPlan_ChangedFileSelectsDefaultLane(t *testing.T) {
	dir := initRepoWithBase(t)
	runGit(t, dir, "checkout", "-q", "-b", "feature")
	writeFile(t, dir, "main.go", "package main\n")
	runGit(t, dir, "add", "-A")
	runGit(t, dir, "commit", "-q", "-m", "add main.go")

	plan, err := planner.BuildPlan(context.Background(), dir, "main", "HEAD", config.Config{})
	if err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}

	if len(plan.ChangedPaths) != 1 || plan.ChangedPaths[0] != "main.go" {
		t.Fatalf("expected changed paths [main.go], got %v", plan.ChangedPaths)
	}
	test := stageDecision(t, plan, "test")
	if test.Action != "run" {
		t.Fatalf("expected test stage to run when a path changed, got %+v", test)
	}
}

func TestBuildPlan_RenameIsClassifiedByBothOldAndNewPath(t *testing.T) {
	dir := initRepoWithBase(t)
	writeFile(t, dir, "old.go", "package main\n")
	runGit(t, dir, "add", "-A")
	runGit(t, dir, "commit", "-q", "-m", "add old.go")
	runGit(t, dir, "checkout", "-q", "-b", "feature")
	runGit(t, dir, "mv", "old.go", "new.go")
	runGit(t, dir, "commit", "-q", "-m", "rename old.go to new.go")

	plan, err := planner.BuildPlan(context.Background(), dir, "main", "HEAD", config.Config{})
	if err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}

	wantOld, wantNew := false, false
	for _, p := range plan.ChangedPaths {
		if p == "old.go" {
			wantOld = true
		}
		if p == "new.go" {
			wantNew = true
		}
	}
	if !wantOld || !wantNew {
		t.Fatalf("expected changed paths to include both old.go and new.go, got %v", plan.ChangedPaths)
	}
}

func TestBuildPlan_FixedStagesAlwaysRunWithExplicitReasons(t *testing.T) {
	dir := initRepoWithBase(t)

	plan, err := planner.BuildPlan(context.Background(), dir, "main", "HEAD", config.Config{})
	if err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}

	for _, name := range []string{"intent", "rebase", "review", "push", "pr", "ci"} {
		d := stageDecision(t, plan, name)
		if d.Action != "run" {
			t.Fatalf("expected stage %q to always run, got %+v", name, d)
		}
		if d.Reason == "" {
			t.Fatalf("expected stage %q to carry a concrete reason, got empty string", name)
		}
	}
}

func TestBuildPlan_IsDeterministicForIdenticalInputs(t *testing.T) {
	dir := initRepoWithBase(t)
	cfg := config.Config{Commands: config.Commands{Test: "go test ./...", Lint: "go vet ./..."}}

	first, err := planner.BuildPlan(context.Background(), dir, "main", "HEAD", cfg)
	if err != nil {
		t.Fatalf("BuildPlan (first): %v", err)
	}
	second, err := planner.BuildPlan(context.Background(), dir, "main", "HEAD", cfg)
	if err != nil {
		t.Fatalf("BuildPlan (second): %v", err)
	}

	if first.ConfigHash != second.ConfigHash {
		t.Fatalf("expected deterministic config hash, got %q vs %q", first.ConfigHash, second.ConfigHash)
	}
	if first.BaseSHA != second.BaseSHA || first.CandidateSHA != second.CandidateSHA {
		t.Fatalf("expected deterministic SHAs across identical calls")
	}
}

func TestBuildPlan_DifferentConfigChangesHash(t *testing.T) {
	dir := initRepoWithBase(t)

	a, err := planner.BuildPlan(context.Background(), dir, "main", "HEAD", config.Config{Commands: config.Commands{Test: "go test ./..."}})
	if err != nil {
		t.Fatalf("BuildPlan (a): %v", err)
	}
	b, err := planner.BuildPlan(context.Background(), dir, "main", "HEAD", config.Config{Commands: config.Commands{Test: "go test ./cmd/..."}})
	if err != nil {
		t.Fatalf("BuildPlan (b): %v", err)
	}

	if a.ConfigHash == b.ConfigHash {
		t.Fatalf("expected different effective config to change the config hash")
	}
}

func TestBuildPlan_DoesNotMutateRepositoryState(t *testing.T) {
	dir := initRepoWithBase(t)
	before := runGit(t, dir, "status", "--porcelain", "--branch")

	if _, err := planner.BuildPlan(context.Background(), dir, "main", "HEAD", config.Config{}); err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}

	after := runGit(t, dir, "status", "--porcelain", "--branch")
	if before != after {
		t.Fatalf("expected BuildPlan to be side-effect-free, git status changed:\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

func TestBuildPlan_ConfiguredLanesSelectByPath(t *testing.T) {
	dir := initRepoWithBase(t)
	runGit(t, dir, "checkout", "-q", "-b", "feature")
	writeFile(t, dir, "main.go", "package main\n")
	runGit(t, dir, "add", "-A")
	runGit(t, dir, "commit", "-q", "-m", "add main.go")

	cfg := config.Config{
		Validation: config.Validation{
			Lanes: map[string]config.Lane{
				"go":   {Paths: []string{"**/*.go"}, RequiredBeforePush: true},
				"docs": {Paths: []string{"**/*.md"}, RequiredBeforePush: true},
			},
		},
	}

	plan, err := planner.BuildPlan(context.Background(), dir, "main", "HEAD", cfg)
	if err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}

	if len(plan.Lanes) != 2 {
		t.Fatalf("expected 2 lane decisions, got %+v", plan.Lanes)
	}
	var goLane, docsLane *planner.LaneDecision
	for i := range plan.Lanes {
		switch plan.Lanes[i].Name {
		case "go":
			goLane = &plan.Lanes[i]
		case "docs":
			docsLane = &plan.Lanes[i]
		}
	}
	if goLane == nil || goLane.Action != "run" {
		t.Fatalf("expected go lane to run, got %+v", goLane)
	}
	if docsLane == nil || docsLane.Action != "skip" {
		t.Fatalf("expected docs lane to skip, got %+v", docsLane)
	}
	test := stageDecision(t, plan, "test")
	if test.Action != "run" {
		t.Fatalf("expected test stage to run because the go lane matched, got %+v", test)
	}
}

func stageDecision(t *testing.T, plan planner.Plan, name string) planner.StageDecision {
	t.Helper()
	for _, s := range plan.Stages {
		if s.Name == name {
			return s
		}
	}
	t.Fatalf("no stage decision found for %q in %+v", name, plan.Stages)
	return planner.StageDecision{}
}
