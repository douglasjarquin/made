package gitgate_test

import (
	"path/filepath"
	"testing"

	"github.com/douglasjarquin/made/internal/gitgate"
)

func TestGatePathIsDeterministicPerRepo(t *testing.T) {
	a1 := gitgate.GatePath("/home/doug/.made", "github.com/douglasjarquin/made")
	a2 := gitgate.GatePath("/home/doug/.made", "github.com/douglasjarquin/made")
	b := gitgate.GatePath("/home/doug/.made", "github.com/douglasjarquin/herdr")

	if a1 != a2 {
		t.Fatalf("expected same repo identifier to produce the same path, got %q and %q", a1, a2)
	}
	if a1 == b {
		t.Fatalf("expected different repo identifiers to produce different paths, both got %q", a1)
	}
	if filepath.Base(a1) != "gate.git" {
		t.Fatalf("expected gate path to end in gate.git, got %q", a1)
	}
	if filepath.Dir(filepath.Dir(a1)) != filepath.Join("/home/doug/.made", "gates") {
		t.Fatalf("expected gate path under <madeHome>/gates/<hash>/gate.git, got %q", a1)
	}
}

func TestWorktreesDirIsSiblingOfGateGit(t *testing.T) {
	gatePath := gitgate.GatePath("/home/doug/.made", "github.com/douglasjarquin/made")
	got := gitgate.WorktreesDir(gatePath)
	want := filepath.Join(filepath.Dir(gatePath), "worktrees")
	if got != want {
		t.Fatalf("WorktreesDir(%q) = %q, want %q", gatePath, got, want)
	}
}
