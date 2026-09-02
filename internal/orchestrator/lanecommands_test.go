package orchestrator

import (
	"context"
	"testing"

	"github.com/douglasjarquin/made/internal/config"
	"github.com/douglasjarquin/made/internal/gitgate"
)

func TestLaneFullCommandsForTest_NoLanesConfiguredReturnsNil(t *testing.T) {
	dir := gitInitWithCommit(t, "base")
	c := &chain{ctx: context.Background(), defaultBranch: "main", rc: &RunContext{Config: config.Config{}, Worktree: &gitgate.Worktree{Path: dir}}}

	extras, err := c.laneFullCommandsForTest()
	if err != nil {
		t.Fatalf("laneFullCommandsForTest: %v", err)
	}
	if extras != nil {
		t.Fatalf("expected nil extras when no lanes are configured, got %+v", extras)
	}
}

func TestLaneFullCommandsForTest_SelectedLaneProducesExtraCommand(t *testing.T) {
	dir := gitInitWithCommit(t, "base")
	runGit(t, dir, "config", "user.email", "orchestrator-test@example.com")
	runGit(t, dir, "config", "user.name", "orchestrator-test")
	runGit(t, dir, "checkout", "-q", "-b", "feature")
	writeFile(t, dir, "main.go", "package main\n")
	runGit(t, dir, "add", "-A")
	runGit(t, dir, "commit", "-q", "-m", "add main.go")

	cfg := config.Config{
		Validation: config.Validation{
			Lanes: map[string]config.Lane{
				"go": {Paths: []string{"**/*.go"}, Full: []string{"echo go-full"}, RequiredBeforePush: true},
			},
		},
	}
	c := &chain{ctx: context.Background(), defaultBranch: "main", rc: &RunContext{Config: cfg, Worktree: &gitgate.Worktree{Path: dir}}}

	extras, err := c.laneFullCommandsForTest()
	if err != nil {
		t.Fatalf("laneFullCommandsForTest: %v", err)
	}
	if len(extras) != 1 {
		t.Fatalf("expected 1 extra command, got %+v", extras)
	}
	if extras[0].Name != "go" {
		t.Fatalf("expected extra command named %q, got %q", "go", extras[0].Name)
	}
}

func TestLaneFullCommandsForTest_UnselectedLaneProducesNoCommand(t *testing.T) {
	dir := gitInitWithCommit(t, "base")
	runGit(t, dir, "config", "user.email", "orchestrator-test@example.com")
	runGit(t, dir, "config", "user.name", "orchestrator-test")
	runGit(t, dir, "checkout", "-q", "-b", "feature")
	writeFile(t, dir, "README.md", "docs change\n")
	runGit(t, dir, "add", "-A")
	runGit(t, dir, "commit", "-q", "-m", "docs change")

	cfg := config.Config{
		Validation: config.Validation{
			Lanes: map[string]config.Lane{
				"go":   {Paths: []string{"**/*.go"}, Full: []string{"echo go-full"}, RequiredBeforePush: true},
				"docs": {Paths: []string{"**/*.md"}, Full: []string{"echo docs-full"}, RequiredBeforePush: true},
			},
		},
	}
	c := &chain{ctx: context.Background(), defaultBranch: "main", rc: &RunContext{Config: cfg, Worktree: &gitgate.Worktree{Path: dir}}}

	extras, err := c.laneFullCommandsForTest()
	if err != nil {
		t.Fatalf("laneFullCommandsForTest: %v", err)
	}
	names := map[string]bool{}
	for _, e := range extras {
		names[e.Name] = true
	}
	if !names["docs"] {
		t.Fatalf("expected the docs lane to produce a command, got %+v", extras)
	}
	if names["go"] {
		t.Fatalf("expected the unselected go lane to produce no command, got %+v", extras)
	}
}
