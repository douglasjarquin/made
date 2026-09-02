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

// TestLaneFullCommandsForTest_RecomputesAfterALaterCommit proves the
// "recompute after Review auto-fixes" requirement (project issue #33 Phase
// 2): laneFullCommandsForTest always diffs against the worktree's current
// HEAD, so a commit added after the candidate was first prepared - such as
// a Review auto-fix - is picked up automatically, without needing Review to
// notify anyone. No real auto-fix machinery is exercised here; what matters
// is that HEAD moving between two calls changes the result.
func TestLaneFullCommandsForTest_RecomputesAfterALaterCommit(t *testing.T) {
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
				"go":   {Paths: []string{"**/*.go"}, Full: []string{"echo go-full"}, RequiredBeforePush: true},
				"docs": {Paths: []string{"**/*.md"}, Full: []string{"echo docs-full"}, RequiredBeforePush: true},
			},
		},
	}
	c := &chain{ctx: context.Background(), defaultBranch: "main", rc: &RunContext{Config: cfg, Worktree: &gitgate.Worktree{Path: dir}}}

	before, err := c.laneFullCommandsForTest()
	if err != nil {
		t.Fatalf("laneFullCommandsForTest (before): %v", err)
	}
	beforeNames := map[string]bool{}
	for _, e := range before {
		beforeNames[e.Name] = true
	}
	if !beforeNames["go"] || beforeNames["docs"] {
		t.Fatalf("expected only the go lane before the auto-fix commit, got %+v", before)
	}

	// Simulate a Review auto-fix: a new commit lands on HEAD after the
	// candidate was first prepared, touching a different lane's paths.
	writeFile(t, dir, "README.md", "auto-fixed docs\n")
	runGit(t, dir, "add", "-A")
	runGit(t, dir, "commit", "-q", "-m", "review auto-fix: update README")

	after, err := c.laneFullCommandsForTest()
	if err != nil {
		t.Fatalf("laneFullCommandsForTest (after): %v", err)
	}
	afterNames := map[string]bool{}
	for _, e := range after {
		afterNames[e.Name] = true
	}
	if !afterNames["go"] || !afterNames["docs"] {
		t.Fatalf("expected both lanes selected after the auto-fix commit moved HEAD, got %+v", after)
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
