package push_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/douglasjarquin/made/internal/gitgate"
)

func commitEnv() []string {
	return []string{
		"GIT_AUTHOR_NAME=push-test",
		"GIT_AUTHOR_EMAIL=push-test@example.com",
		"GIT_COMMITTER_NAME=push-test",
		"GIT_COMMITTER_EMAIL=push-test@example.com",
		"GIT_CONFIG_COUNT=2",
		"GIT_CONFIG_KEY_0=commit.gpgsign",
		"GIT_CONFIG_VALUE_0=false",
		"GIT_CONFIG_KEY_1=safe.bareRepository",
		"GIT_CONFIG_VALUE_1=all",
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

func runAllowFail(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), commitEnv()...)
	out, _ := cmd.CombinedOutput()
	return string(out)
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

// fixture stands in for made's real topology: gatePath is the trust-boundary
// bare repo that worktrees are cut from, remotePath is a second, independent
// bare repo standing in for the real GitHub remote the Push stage targets.
type fixture struct {
	gatePath     string
	remotePath   string
	worktreesDir string
	branch       string
}

func setupFixture(t *testing.T) fixture {
	t.Helper()
	dir := t.TempDir()

	gatePath := filepath.Join(dir, "gate.git")
	if err := gitgate.InitBare(gatePath); err != nil {
		t.Fatalf("InitBare gate: %v", err)
	}

	remotePath := filepath.Join(dir, "remote.git")
	if err := gitgate.InitBare(remotePath); err != nil {
		t.Fatalf("InitBare remote: %v", err)
	}

	srcPath := filepath.Join(dir, "src")
	if err := os.MkdirAll(srcPath, 0o755); err != nil {
		t.Fatalf("mkdir src: %v", err)
	}
	run(t, srcPath, "init", "-q")
	writeFile(t, srcPath, "README.md", "push fixture\n")
	run(t, srcPath, "add", ".")
	run(t, srcPath, "commit", "-q", "-m", "initial commit")
	run(t, srcPath, "branch", "-M", "feature")
	run(t, srcPath, "push", gatePath, "HEAD:refs/heads/feature")

	return fixture{
		gatePath:     gatePath,
		remotePath:   remotePath,
		worktreesDir: filepath.Join(dir, "worktrees"),
		branch:       "feature",
	}
}

func (f fixture) addWorktree(t *testing.T) *gitgate.Worktree {
	t.Helper()
	wt, err := gitgate.AddWorktree(f.gatePath, f.worktreesDir, "refs/heads/"+f.branch)
	if err != nil {
		t.Fatalf("AddWorktree: %v", err)
	}
	return wt
}

func addRemote(t *testing.T, worktreePath, name, url string) {
	t.Helper()
	run(t, worktreePath, "remote", "add", name, url)
}
