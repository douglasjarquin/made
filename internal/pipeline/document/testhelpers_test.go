package document_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/douglasjarquin/made/internal/gitgate"
)

func commitEnv() []string {
	return []string{
		"GIT_AUTHOR_NAME=document-stage-test",
		"GIT_AUTHOR_EMAIL=document-stage-test@example.com",
		"GIT_COMMITTER_NAME=document-stage-test",
		"GIT_COMMITTER_EMAIL=document-stage-test@example.com",
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

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	full := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir for %s: %v", name, err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

type fixture struct {
	barePath     string
	srcPath      string
	worktreesDir string
}

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
	writeFile(t, srcPath, "README.md", "document-stage fixture\n")
	run(t, srcPath, "add", ".")
	run(t, srcPath, "commit", "-q", "-m", "initial commit")
	run(t, srcPath, "branch", "-M", "main")
	run(t, srcPath, "push", barePath, "HEAD:refs/heads/main")

	return fixture{barePath: barePath, srcPath: srcPath, worktreesDir: filepath.Join(dir, "worktrees")}
}

// pushBranch builds a feature branch off main in the source clone, commits
// the given files, and force-pushes it to the bare gate repo so a worktree
// checked out at its tip diverges from main by exactly this commit.
func (f fixture) pushBranch(t *testing.T, branch string, files map[string]string, message string) {
	t.Helper()
	run(t, f.srcPath, "checkout", "-q", "-B", branch, "main")
	for name, content := range files {
		writeFile(t, f.srcPath, name, content)
	}
	run(t, f.srcPath, "add", ".")
	run(t, f.srcPath, "commit", "-q", "-m", message)
	run(t, f.srcPath, "push", "-q", "-f", f.barePath, "HEAD:refs/heads/"+branch)
	run(t, f.srcPath, "checkout", "-q", "main")
}

func (f fixture) addWorktree(t *testing.T, ref string) *gitgate.Worktree {
	t.Helper()
	wt, err := gitgate.AddWorktree(f.barePath, f.worktreesDir, ref)
	if err != nil {
		t.Fatalf("AddWorktree: %v", err)
	}
	return wt
}
