package evidence_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/douglasjarquin/made/internal/evidence"
)

func TestInRepoStore_RejectsPathTraversalAndOversizedEvidence(t *testing.T) {
	store := &evidence.InRepoStore{RepoPath: t.TempDir(), Dir: ".made/evidence"}

	if err := store.WriteEvidence("run-1", map[string][]byte{
		"../escaped.txt": []byte("must stay inside the run"),
	}); err == nil {
		t.Fatal("evidence store accepted a path traversal outside the run evidence directory")
	}

	if err := store.WriteEvidence("run-1", map[string][]byte{
		"large.log": []byte(strings.Repeat("x", 2<<20)),
	}); err == nil {
		t.Fatal("evidence store accepted output beyond the bounded retention limit")
	}
}

func TestInRepoStore_RedactsPublishedSecrets(t *testing.T) {
	repo := t.TempDir()
	store := &evidence.InRepoStore{RepoPath: repo, Dir: ".made/evidence"}
	if err := store.WriteEvidence("run-1", map[string][]byte{"log.txt": []byte("Authorization: Bearer secret-value\n")}); err != nil {
		t.Fatalf("WriteEvidence: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(repo, ".made/evidence", "run-1", "log.txt"))
	if err != nil {
		t.Fatalf("read evidence: %v", err)
	}
	if strings.Contains(string(data), "secret-value") {
		t.Fatalf("published evidence retained an authorization secret: %q", data)
	}
}
