package review_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/douglasjarquin/made/internal/agent"
	"github.com/douglasjarquin/made/internal/gitgate"
)

func commitEnv() []string {
	return []string{
		"GIT_AUTHOR_NAME=review-test",
		"GIT_AUTHOR_EMAIL=review-test@example.com",
		"GIT_COMMITTER_NAME=review-test",
		"GIT_COMMITTER_EMAIL=review-test@example.com",
	}
}

func run(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), commitEnv()...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v in %s failed: %v: %s", args, dir, err, out)
	}
	return string(out)
}

func headSHA(t *testing.T, dir string) string {
	t.Helper()
	return strings.TrimSpace(run(t, dir, "rev-parse", "HEAD"))
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

func writeScenario(t *testing.T, findings agent.Findings) string {
	t.Helper()
	data, err := json.Marshal(findings)
	if err != nil {
		t.Fatalf("marshal scenario: %v", err)
	}
	path := filepath.Join(t.TempDir(), "scenario.json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write scenario: %v", err)
	}
	return path
}

type fixture struct {
	barePath     string
	worktreesDir string
	branch       string
}

// setupFixture seeds a bare gate repo with one file (reviewed.txt) so tests
// have real tracked content to patch; the patch text itself is produced by
// diffing a throwaway clone against this same base commit, guaranteeing it is
// a real unified diff `git apply` accepts rather than hand-authored text that
// could silently drift from valid patch syntax.
func setupFixture(t *testing.T) fixture {
	t.Helper()
	dir := t.TempDir()
	barePath := filepath.Join(dir, "gate.git")
	if err := gitgate.InitBare(barePath); err != nil {
		t.Fatalf("InitBare: %v", err)
	}

	srcPath := filepath.Join(dir, "src")
	if err := os.MkdirAll(srcPath, 0o755); err != nil {
		t.Fatalf("mkdir src: %v", err)
	}
	run(t, srcPath, "init", "-q")
	writeFile(t, srcPath, "reviewed.txt", "line one\n")
	run(t, srcPath, "add", ".")
	run(t, srcPath, "commit", "-q", "-m", "initial commit")
	run(t, srcPath, "branch", "-M", "main")
	run(t, srcPath, "push", barePath, "HEAD:refs/heads/main")

	return fixture{barePath: barePath, worktreesDir: filepath.Join(dir, "worktrees"), branch: "refs/heads/main"}
}

func (f fixture) addWorktree(t *testing.T) *gitgate.Worktree {
	t.Helper()
	wt, err := gitgate.AddWorktree(f.barePath, f.worktreesDir, f.branch)
	if err != nil {
		t.Fatalf("AddWorktree: %v", err)
	}
	return wt
}

// autoFixPatch produces a real unified diff (via a throwaway clone of the
// worktree at its current HEAD) that appends a line to reviewed.txt, so
// review.Run's `git apply` step in the test worktree has a genuinely valid
// patch to apply rather than a hand-authored one.
func autoFixPatch(t *testing.T, worktreePath string) string {
	t.Helper()
	scratch := t.TempDir()
	run(t, scratch, "clone", "-q", worktreePath, ".")
	writeFile(t, scratch, "reviewed.txt", "line one\nauto-fixed line\n")
	return run(t, scratch, "diff")
}
