package orchestrator

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/douglasjarquin/made/internal/evidence"
)

func gitInitWithCommit(t *testing.T, subject string) string {
	t.Helper()
	dir := t.TempDir()
	runGit(t, dir, "init", "-q", "-b", "main")
	writeFile(t, dir, "file.txt", "content\n")
	runGit(t, dir, "add", "-A")
	cmd := exec.Command("git", "commit", "-q", "-m", subject)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=orchestrator-test",
		"GIT_AUTHOR_EMAIL=orchestrator-test@example.com",
		"GIT_COMMITTER_NAME=orchestrator-test",
		"GIT_COMMITTER_EMAIL=orchestrator-test@example.com",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git commit in %s failed: %v: %s", dir, err, out)
	}
	return dir
}

func TestDerivePRTitle_ReturnsCommitSubject(t *testing.T) {
	dir := gitInitWithCommit(t, "add the greeting feature")

	title, err := derivePRTitle(dir)
	if err != nil {
		t.Fatalf("derivePRTitle: %v", err)
	}
	if title != "add the greeting feature" {
		t.Fatalf("expected title %q, got %q", "add the greeting feature", title)
	}
}

func TestDerivePRTitle_TrimsWhitespace(t *testing.T) {
	dir := gitInitWithCommit(t, "  trailing subject  ")

	title, err := derivePRTitle(dir)
	if err != nil {
		t.Fatalf("derivePRTitle: %v", err)
	}
	if title != "trailing subject" {
		t.Fatalf("expected trimmed title %q, got %q", "trailing subject", title)
	}
}

func TestDerivePRTitle_NotAGitRepoReturnsError(t *testing.T) {
	dir := t.TempDir()

	if _, err := derivePRTitle(dir); err == nil {
		t.Fatal("expected an error deriving a PR title outside a git repo")
	}
}

func TestDeriveEvidenceRef_OrphanBranchStore(t *testing.T) {
	store := &evidence.OrphanBranchStore{RepoPath: t.TempDir(), Branch: "custom-evidence-branch"}

	ref := deriveEvidenceRef(store, "run-123")

	want := "refs/heads/custom-evidence-branch:run-123"
	if ref != want {
		t.Fatalf("expected %q, got %q", want, ref)
	}
}

func TestDeriveEvidenceRef_OrphanBranchStore_DefaultsBranch(t *testing.T) {
	store := &evidence.OrphanBranchStore{RepoPath: t.TempDir()}

	ref := deriveEvidenceRef(store, "run-456")

	want := "refs/heads/" + evidence.DefaultBranch + ":run-456"
	if ref != want {
		t.Fatalf("expected %q, got %q", want, ref)
	}
}

func TestDeriveEvidenceRef_InRepoStore(t *testing.T) {
	store := &evidence.InRepoStore{RepoPath: t.TempDir(), Dir: ".made/custom-evidence"}

	ref := deriveEvidenceRef(store, "run-789")

	want := filepath.Join(".made/custom-evidence", "run-789")
	if ref != want {
		t.Fatalf("expected %q, got %q", want, ref)
	}
}

func TestDeriveEvidenceRef_InRepoStore_DefaultsDir(t *testing.T) {
	store := &evidence.InRepoStore{RepoPath: t.TempDir()}

	ref := deriveEvidenceRef(store, "run-999")

	want := filepath.Join(evidence.DefaultDir, "run-999")
	if ref != want {
		t.Fatalf("expected %q, got %q", want, ref)
	}
}

func TestDeriveEvidenceRef_DistinguishesModes(t *testing.T) {
	orphan := deriveEvidenceRef(&evidence.OrphanBranchStore{RepoPath: t.TempDir()}, "run-x")
	inRepo := deriveEvidenceRef(&evidence.InRepoStore{RepoPath: t.TempDir()}, "run-x")

	if orphan == inRepo {
		t.Fatalf("expected distinguishable references for the two evidence modes, both were %q", orphan)
	}
}
