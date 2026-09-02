package orchestrator

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/douglasjarquin/made/internal/config"
	"github.com/douglasjarquin/made/internal/gitgate"
)

func addOrigin(t *testing.T, dir string) {
	t.Helper()
	remoteParent := t.TempDir()
	remote := filepath.Join(remoteParent, "remote.git")
	runGit(t, remoteParent, "init", "-q", "--bare", remote)
	runGit(t, dir, "remote", "add", "origin", remote)
}

func TestLaneFullCommandsForTest_NoLanesConfiguredReturnsNil(t *testing.T) {
	dir := gitInitWithCommit(t, "base")
	c := &chain{ctx: context.Background(), defaultBranch: "main", rc: &RunContext{Config: config.Config{}, Worktree: &gitgate.Worktree{Path: dir}}}

	plan, err := c.laneFullCommandsForTest()
	if err != nil {
		t.Fatalf("laneFullCommandsForTest: %v", err)
	}
	if plan.Extras != nil || plan.Reused != nil {
		t.Fatalf("expected an empty plan when no lanes are configured, got %+v", plan)
	}
}

func TestLaneFullCommandsForTest_SelectedLaneProducesExtraCommand(t *testing.T) {
	dir := gitInitWithCommit(t, "base")
	runGit(t, dir, "config", "user.email", "orchestrator-test@example.com")
	runGit(t, dir, "config", "user.name", "orchestrator-test")
	addOrigin(t, dir)
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

	plan, err := c.laneFullCommandsForTest()
	if err != nil {
		t.Fatalf("laneFullCommandsForTest: %v", err)
	}
	if len(plan.Extras) != 1 {
		t.Fatalf("expected 1 extra command, got %+v", plan)
	}
	if plan.Extras[0].Name != "go" {
		t.Fatalf("expected extra command named %q, got %q", "go", plan.Extras[0].Name)
	}
	if len(plan.Reused) != 0 {
		t.Fatalf("expected nothing reused on a first run, got %+v", plan.Reused)
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
	addOrigin(t, dir)
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
	for _, e := range before.Extras {
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
	for _, e := range after.Extras {
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
	addOrigin(t, dir)
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

	plan, err := c.laneFullCommandsForTest()
	if err != nil {
		t.Fatalf("laneFullCommandsForTest: %v", err)
	}
	names := map[string]bool{}
	for _, e := range plan.Extras {
		names[e.Name] = true
	}
	if !names["docs"] {
		t.Fatalf("expected the docs lane to produce a command, got %+v", plan)
	}
	if names["go"] {
		t.Fatalf("expected the unselected go lane to produce no command, got %+v", plan)
	}
}

func setupLaneReuseFixture(t *testing.T) (dir string, cfg config.Config, c *chain) {
	t.Helper()
	dir = gitInitWithCommit(t, "base")
	runGit(t, dir, "config", "user.email", "orchestrator-test@example.com")
	runGit(t, dir, "config", "user.name", "orchestrator-test")
	addOrigin(t, dir)
	runGit(t, dir, "checkout", "-q", "-b", "feature")
	writeFile(t, dir, "main.go", "package main\n")
	runGit(t, dir, "add", "-A")
	runGit(t, dir, "commit", "-q", "-m", "add main.go")

	cfg = config.Config{
		Validation: config.Validation{
			Lanes: map[string]config.Lane{
				"go": {Paths: []string{"**/*.go"}, Full: []string{"echo go-full"}, RequiredBeforePush: true},
			},
		},
	}
	c = &chain{ctx: context.Background(), defaultBranch: "main", runID: "run-reuse-fixture", rc: &RunContext{Config: cfg, Worktree: &gitgate.Worktree{Path: dir}}}
	return dir, cfg, c
}

func TestLaneFullCommandsForTest_ReusesAfterAPublishedReceipt(t *testing.T) {
	_, _, c := setupLaneReuseFixture(t)

	first, err := c.laneFullCommandsForTest()
	if err != nil {
		t.Fatalf("laneFullCommandsForTest (first): %v", err)
	}
	if len(first.Extras) != 1 {
		t.Fatalf("expected 1 extra command before any receipt exists, got %+v", first)
	}
	c.publishLaneReceipts(first)

	second, err := c.laneFullCommandsForTest()
	if err != nil {
		t.Fatalf("laneFullCommandsForTest (second): %v", err)
	}
	if len(second.Extras) != 0 {
		t.Fatalf("expected no extra commands once a receipt was published, got %+v", second)
	}
	if len(second.Reused) != 1 || second.Reused[0] != "go" {
		t.Fatalf("expected the go lane to be reported reused, got %+v", second)
	}
}

func TestLaneFullCommandsForTest_NoReuseIgnoresPublishedReceipt(t *testing.T) {
	dir, cfg, c := setupLaneReuseFixture(t)

	first, err := c.laneFullCommandsForTest()
	if err != nil {
		t.Fatalf("laneFullCommandsForTest (first): %v", err)
	}
	c.publishLaneReceipts(first)

	cfg.Validation.NoReuse = true
	c2 := &chain{ctx: context.Background(), defaultBranch: "main", runID: "run-no-reuse", rc: &RunContext{Config: cfg, Worktree: &gitgate.Worktree{Path: dir}}}

	second, err := c2.laneFullCommandsForTest()
	if err != nil {
		t.Fatalf("laneFullCommandsForTest (second): %v", err)
	}
	if len(second.Extras) != 1 {
		t.Fatalf("expected NoReuse to force re-execution despite a published receipt, got %+v", second)
	}
	if len(second.Reused) != 0 {
		t.Fatalf("expected nothing marked reused under NoReuse, got %+v", second.Reused)
	}
}

// TestResolveWorktreeRef_ScopesToGivenRepoPath guards against a real
// regression: resolveWorktreeRef must run git in repoPath, not in whatever
// directory the calling process happens to be in. Two independent repos
// with the same ref name ("main") but different commits at that ref prove
// the function reads the one it was actually asked to.
func TestResolveWorktreeRef_ScopesToGivenRepoPath(t *testing.T) {
	repoA := gitInitWithCommit(t, "repo A commit")
	repoB := gitInitWithCommit(t, "repo B commit")

	shaA, err := resolveWorktreeRef(context.Background(), repoA, "main")
	if err != nil {
		t.Fatalf("resolveWorktreeRef(repoA): %v", err)
	}
	shaB, err := resolveWorktreeRef(context.Background(), repoB, "main")
	if err != nil {
		t.Fatalf("resolveWorktreeRef(repoB): %v", err)
	}
	if shaA == shaB {
		t.Fatalf("expected different repos' \"main\" to resolve to different commits, both resolved to %q", shaA)
	}

	wantA := revParseInDir(t, repoA, "main")
	if shaA != wantA {
		t.Fatalf("resolveWorktreeRef(repoA) = %q, want %q (repoA's own HEAD)", shaA, wantA)
	}
}

func revParseInDir(t *testing.T, dir, ref string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", dir, "rev-parse", ref).Output()
	if err != nil {
		t.Fatalf("git -C %s rev-parse %s: %v", dir, ref, err)
	}
	return strings.TrimSpace(string(out))
}

func TestPublishLaneReceipts_NoExtrasIsANoOp(t *testing.T) {
	_, _, c := setupLaneReuseFixture(t)
	c.publishLaneReceipts(laneTestPlan{})
	if _, err := os.Stat(c.rc.Worktree.Path); err != nil {
		t.Fatalf("expected the worktree to be untouched: %v", err)
	}
}
