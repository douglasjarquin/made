package evidence_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/douglasjarquin/made/internal/evidence"
)

func TestOrphanBranchStore_ConcurrentWritesRetainBothRuns(t *testing.T) {
	repo := t.TempDir()
	initGitRepo(t, repo)
	store := &evidence.OrphanBranchStore{RepoPath: repo}

	start := make(chan struct{})
	errCh := make(chan error, 2)
	var wg sync.WaitGroup
	for _, runID := range []string{"run-a", "run-b"} {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			<-start
			errCh <- store.WriteEvidence(id, map[string][]byte{
				"result.json": fmt.Appendf(nil, `{"run_id":%q}`, id),
			})
		}(runID)
	}
	close(start)
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			t.Fatalf("concurrent evidence write failed: %v", err)
		}
	}

	tree := gitOutput(t, repo, "ls-tree", "-r", "--name-only", "refs/heads/made-evidence")
	for _, runID := range []string{"run-a", "run-b"} {
		if !containsLine(tree, filepath.Join(runID, "result.json")) {
			t.Fatalf("evidence branch missing %s: %s", runID, tree)
		}
	}
}

func TestInRepoStoreRejectsSymlinkedEvidenceDirectory(t *testing.T) {
	repo := t.TempDir()
	outside := t.TempDir()
	evidenceRoot := filepath.Join(repo, ".made", "evidence")
	if err := os.MkdirAll(filepath.Dir(evidenceRoot), 0o755); err != nil {
		t.Fatalf("create evidence parent: %v", err)
	}
	if err := os.Symlink(outside, evidenceRoot); err != nil {
		t.Fatalf("create evidence symlink: %v", err)
	}

	store := &evidence.InRepoStore{RepoPath: repo, Dir: ".made/evidence"}
	if err := store.WriteEvidence("run-escape", map[string][]byte{"result.json": []byte("secret")}); err == nil {
		t.Fatal("WriteEvidence accepted a symlinked evidence directory")
	}
	if _, err := os.Stat(filepath.Join(outside, "run-escape", "result.json")); !os.IsNotExist(err) {
		t.Fatalf("symlinked evidence directory received data: err=%v", err)
	}
}

func initGitRepo(t *testing.T, dir string) {
	t.Helper()
	gitOutput(t, dir, "init", "-q")
	gitOutput(t, dir, "-c", "user.name=fixture", "-c", "user.email=fixture@example.com", "commit", "--allow-empty", "-m", "initial")
}

func gitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(cmd.Env, "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null", "SSH_AUTH_SOCK=")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return string(out)
}

func containsLine(output, want string) bool {
	return slices.Contains(strings.Split(output, "\n"), want)
}
