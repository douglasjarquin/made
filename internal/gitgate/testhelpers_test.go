package gitgate_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func initSourceRepo(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("mkdir source repo: %v", err)
	}
	run(t, path, nil, "git", "init", "-q")
	if err := os.WriteFile(filepath.Join(path, "README.md"), []byte("gate fixture\n"), 0o644); err != nil {
		t.Fatalf("write fixture file: %v", err)
	}
	run(t, path, nil, "git", "add", "README.md")
	run(t, path, commitEnv(), "git", "commit", "-q", "-m", "initial commit")
}

func commitEnv() []string {
	return []string{
		"GIT_AUTHOR_NAME=gitgate-test",
		"GIT_AUTHOR_EMAIL=gitgate-test@example.com",
		"GIT_COMMITTER_NAME=gitgate-test",
		"GIT_COMMITTER_EMAIL=gitgate-test@example.com",
	}
}

func run(t *testing.T, dir string, extraEnv []string, name string, args ...string) string {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	env := os.Environ()
	if extraEnv != nil {
		env = append(env, extraEnv...)
	}
	// Ensure safe.bareRepository and gpgsign are configured for git operations in tests.
	if name == "git" {
		env = append(env,
			"GIT_CONFIG_COUNT=2",
			"GIT_CONFIG_KEY_0=commit.gpgsign",
			"GIT_CONFIG_VALUE_0=false",
			"GIT_CONFIG_KEY_1=safe.bareRepository",
			"GIT_CONFIG_VALUE_1=all",
		)
	}
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v in %s failed: %v: %s", name, args, dir, err, out)
	}
	return string(out)
}

func pushRef(dir, remote string) (string, error) {
	cmd := exec.Command("git", "push", remote, "HEAD:refs/heads/main")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_COUNT=1",
		"GIT_CONFIG_KEY_0=safe.bareRepository",
		"GIT_CONFIG_VALUE_0=all",
	)
	out, err := cmd.CombinedOutput()
	return string(out), err
}
