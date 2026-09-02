package evidence_test

import (
	"context"
	"path/filepath"
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

func TestOrphanBranchStorePublishesEvidenceToOrigin(t *testing.T) {
	repo := initTargetRepo(t)
	remote := filepath.Join(t.TempDir(), "remote.git")
	run(t, t.TempDir(), "git", "init", "--bare", "-q", remote)
	run(t, repo, "git", "remote", "add", "origin", remote)
	store := &evidence.OrphanBranchStore{RepoPath: repo, Branch: "made-evidence"}
	if err := store.WriteEvidence("run-remote", map[string][]byte{"summary.txt": []byte("published\n")}); err != nil {
		t.Fatalf("WriteEvidence: %v", err)
	}
	if err := store.PublishEvidence("run-remote"); err != nil {
		t.Fatalf("PublishEvidence: %v", err)
	}
	tree := run(t, remote, "git", "ls-tree", "-r", "--name-only", "refs/heads/made-evidence")
	if !strings.Contains(tree, "run-remote/summary.txt") {
		t.Fatalf("remote evidence branch lacks published file: %s", tree)
	}
}

func TestOrphanBranchStorePublishEvidenceSHA_ReturnsCommitActuallyOnOrigin(t *testing.T) {
	repo := initTargetRepo(t)
	remote := filepath.Join(t.TempDir(), "remote.git")
	run(t, t.TempDir(), "git", "init", "--bare", "-q", remote)
	run(t, repo, "git", "remote", "add", "origin", remote)
	store := &evidence.OrphanBranchStore{RepoPath: repo, Branch: "made-evidence"}
	if err := store.WriteEvidence("run-a", map[string][]byte{"summary.txt": []byte("a\n")}); err != nil {
		t.Fatalf("WriteEvidence run-a: %v", err)
	}

	sha, err := store.PublishEvidenceSHA(context.Background(), "run-a")
	if err != nil {
		t.Fatalf("PublishEvidenceSHA: %v", err)
	}
	if strings.TrimSpace(sha) == "" {
		t.Fatal("expected a non-empty commit SHA")
	}

	// A concurrent run advances the local branch AFTER we published, the way
	// a second run's WriteEvidence would race with this run's own PR stage.
	// The SHA already returned to this run must still name a commit that is
	// actually reachable on origin - not the (unpublished) new tip.
	if err := store.WriteEvidence("run-b", map[string][]byte{"summary.txt": []byte("b\n")}); err != nil {
		t.Fatalf("WriteEvidence run-b: %v", err)
	}
	newTip := strings.TrimSpace(run(t, repo, "git", "rev-parse", "refs/heads/made-evidence"))
	if newTip == sha {
		t.Fatal("test setup invalid: expected the second write to advance the branch tip")
	}

	if _, err := runNoFatal(remote, "git", "cat-file", "-e", sha); err != nil {
		t.Fatalf("expected published SHA %s to exist on origin: %v", sha, err)
	}
	if _, err := runNoFatal(remote, "git", "cat-file", "-e", newTip); err == nil {
		t.Fatalf("expected the later, unpublished tip %s to NOT exist on origin yet", newTip)
	}
}
