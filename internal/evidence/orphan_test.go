package evidence_test

import (
	"strings"
	"testing"

	"github.com/douglasjarquin/made/internal/evidence"
)

func TestOrphanBranchStoreIsolatedFromDefaultBranch(t *testing.T) {
	repo := initTargetRepo(t)

	store := &evidence.OrphanBranchStore{RepoPath: repo, Branch: "made-evidence"}
	files := map[string][]byte{
		"findings.md": []byte("# findings\n"),
		"logs/ci.log": []byte("test output\n"),
	}
	if err := store.WriteEvidence("run-1", files); err != nil {
		t.Fatalf("WriteEvidence: %v", err)
	}

	defaultTree := run(t, repo, "git", "ls-tree", "-r", "--name-only", "main")
	if strings.Contains(defaultTree, "findings.md") || strings.Contains(defaultTree, "ci.log") {
		t.Fatalf("expected evidence files absent from default branch tree, got:\n%s", defaultTree)
	}

	evidenceTree := run(t, repo, "git", "ls-tree", "-r", "--name-only", "made-evidence")
	if !strings.Contains(evidenceTree, "run-1/findings.md") {
		t.Fatalf("expected run-1/findings.md on evidence branch, got:\n%s", evidenceTree)
	}
	if !strings.Contains(evidenceTree, "run-1/logs/ci.log") {
		t.Fatalf("expected run-1/logs/ci.log on evidence branch, got:\n%s", evidenceTree)
	}

	head := strings.TrimSpace(run(t, repo, "git", "symbolic-ref", "HEAD"))
	if head != "refs/heads/main" {
		t.Fatalf("expected HEAD to remain on main, got %q", head)
	}
	status := run(t, repo, "git", "status", "--porcelain")
	if strings.TrimSpace(status) != "" {
		t.Fatalf("expected clean working tree/index after orphan write, got:\n%s", status)
	}
}

func TestOrphanBranchStoreAccumulatesAcrossRuns(t *testing.T) {
	repo := initTargetRepo(t)
	store := &evidence.OrphanBranchStore{RepoPath: repo, Branch: "made-evidence"}

	if err := store.WriteEvidence("run-1", map[string][]byte{"a.txt": []byte("a")}); err != nil {
		t.Fatalf("WriteEvidence run-1: %v", err)
	}
	if err := store.WriteEvidence("run-2", map[string][]byte{"b.txt": []byte("b")}); err != nil {
		t.Fatalf("WriteEvidence run-2: %v", err)
	}

	tree := run(t, repo, "git", "ls-tree", "-r", "--name-only", "made-evidence")
	if !strings.Contains(tree, "run-1/a.txt") {
		t.Fatalf("expected run-1/a.txt to persist on evidence branch, got:\n%s", tree)
	}
	if !strings.Contains(tree, "run-2/b.txt") {
		t.Fatalf("expected run-2/b.txt on evidence branch, got:\n%s", tree)
	}
}

func TestOrphanBranchStoreDefaultBranchName(t *testing.T) {
	repo := initTargetRepo(t)
	store := &evidence.OrphanBranchStore{RepoPath: repo}

	if err := store.WriteEvidence("run-1", map[string][]byte{"a.txt": []byte("a")}); err != nil {
		t.Fatalf("WriteEvidence: %v", err)
	}

	if _, err := runNoFatal(repo, "git", "rev-parse", "--verify", "refs/heads/"+evidence.DefaultBranch); err != nil {
		t.Fatalf("expected default evidence branch %q to exist: %v", evidence.DefaultBranch, err)
	}
}
