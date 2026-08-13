package lint_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/douglasjarquin/made/internal/gitgate"
)

func commitEnv() []string {
	return []string{
		"GIT_AUTHOR_NAME=lint-stage-test",
		"GIT_AUTHOR_EMAIL=lint-stage-test@example.com",
		"GIT_COMMITTER_NAME=lint-stage-test",
		"GIT_COMMITTER_EMAIL=lint-stage-test@example.com",
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
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

type fixture struct {
	barePath     string
	worktreesDir string
	branch       string
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
	writeFile(t, srcPath, "README.md", "lint-stage fixture\n")
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
