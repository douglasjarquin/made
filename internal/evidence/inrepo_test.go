package evidence_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/douglasjarquin/made/internal/evidence"
)

func TestInRepoStoreWritesAddableWorkingTreeFiles(t *testing.T) {
	repo := initTargetRepo(t)

	store := &evidence.InRepoStore{RepoPath: repo, Dir: ".made/evidence"}
	files := map[string][]byte{
		"findings.md": []byte("# findings\n"),
		"logs/ci.log": []byte("test output\n"),
	}
	if err := store.WriteEvidence("run-1", files); err != nil {
		t.Fatalf("WriteEvidence: %v", err)
	}

	findingsPath := filepath.Join(repo, ".made", "evidence", "run-1", "findings.md")
	got, err := os.ReadFile(findingsPath)
	if err != nil {
		t.Fatalf("read written evidence file: %v", err)
	}
	if string(got) != "# findings\n" {
		t.Fatalf("unexpected evidence file contents: %q", got)
	}
	logPath := filepath.Join(repo, ".made", "evidence", "run-1", "logs", "ci.log")
	if _, err := os.Stat(logPath); err != nil {
		t.Fatalf("expected nested evidence file to exist: %v", err)
	}

	status := run(t, repo, "git", "status", "--porcelain")
	if !strings.Contains(status, ".made/") {
		t.Fatalf("expected evidence dir to show as untracked/addable in git status, got:\n%s", status)
	}
	if !strings.HasPrefix(strings.TrimSpace(status), "??") {
		t.Fatalf("expected evidence files to be untracked (not auto-committed), got:\n%s", status)
	}

	addStatus := run(t, repo, "git", "add", "-A", "-n", ".made")
	if !strings.Contains(addStatus, "findings.md") {
		t.Fatalf("expected evidence files addable via git add, got:\n%s", addStatus)
	}
}

func TestInRepoStoreDefaultDir(t *testing.T) {
	repo := initTargetRepo(t)
	store := &evidence.InRepoStore{RepoPath: repo}

	if err := store.WriteEvidence("run-1", map[string][]byte{"a.txt": []byte("a")}); err != nil {
		t.Fatalf("WriteEvidence: %v", err)
	}

	want := filepath.Join(repo, evidence.DefaultDir, "run-1", "a.txt")
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("expected default dir evidence file at %s: %v", want, err)
	}
}
