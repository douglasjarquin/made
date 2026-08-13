package rebase_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/douglasjarquin/made/internal/gitgate"
)

func commitEnv() []string {
	return []string{
		"GIT_AUTHOR_NAME=rebase-test",
		"GIT_AUTHOR_EMAIL=rebase-test@example.com",
		"GIT_COMMITTER_NAME=rebase-test",
		"GIT_COMMITTER_EMAIL=rebase-test@example.com",
	}
}

func run(t *testing.T, dir string, args ...string) string {
	t.Helper()
	return runWithEnv(t, dir, commitEnv(), args...)
}

func runWithEnv(t *testing.T, dir string, extraEnv []string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), extraEnv...)
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
	barePath      string
	worktreesDir  string
	defaultBranch string
	pushedBranch  string
}

func setupFixture(t *testing.T, sharedFile string, mainContent, featureContent string) fixture {
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
	writeFile(t, srcPath, "README.md", "rebase fixture\n")
	if sharedFile != "" {
		writeFile(t, srcPath, sharedFile, "base content\n")
	}
	run(t, srcPath, "add", ".")
	run(t, srcPath, "commit", "-q", "-m", "initial commit")
	run(t, srcPath, "branch", "-M", "main")
	run(t, srcPath, "push", barePath, "HEAD:refs/heads/main")

	run(t, srcPath, "checkout", "-q", "-b", "feature")
	writeFile(t, srcPath, "feature-only.txt", "feature file\n")
	if sharedFile != "" {
		writeFile(t, srcPath, sharedFile, featureContent)
	}
	run(t, srcPath, "add", ".")
	run(t, srcPath, "commit", "-q", "-m", "feature commit")
	run(t, srcPath, "push", barePath, "HEAD:refs/heads/feature")

	run(t, srcPath, "checkout", "-q", "main")
	writeFile(t, srcPath, "main-only.txt", "main file\n")
	if sharedFile != "" {
		writeFile(t, srcPath, sharedFile, mainContent)
	}
	run(t, srcPath, "add", ".")
	run(t, srcPath, "commit", "-q", "-m", "main commit")
	run(t, srcPath, "push", barePath, "HEAD:refs/heads/main")

	return fixture{
		barePath:      barePath,
		worktreesDir:  filepath.Join(dir, "worktrees"),
		defaultBranch: "main",
		pushedBranch:  "refs/heads/feature",
	}
}

func (f fixture) addWorktree(t *testing.T) *gitgate.Worktree {
	t.Helper()
	wt, err := gitgate.AddWorktree(f.barePath, f.worktreesDir, f.pushedBranch)
	if err != nil {
		t.Fatalf("AddWorktree: %v", err)
	}
	return wt
}
