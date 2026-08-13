package intent_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func initRepoWithCommit(t *testing.T, message string) string {
	t.Helper()
	dir := t.TempDir()
	run(t, dir, nil, "git", "init", "-q")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("intent fixture\n"), 0o644); err != nil {
		t.Fatalf("write fixture file: %v", err)
	}
	run(t, dir, nil, "git", "add", "README.md")
	run(t, dir, commitEnv(), "git", "commit", "-q", "-m", message)
	return dir
}

func commitEnv() []string {
	return []string{
		"GIT_AUTHOR_NAME=intent-test",
		"GIT_AUTHOR_EMAIL=intent-test@example.com",
		"GIT_COMMITTER_NAME=intent-test",
		"GIT_COMMITTER_EMAIL=intent-test@example.com",
	}
}

func run(t *testing.T, dir string, extraEnv []string, name string, args ...string) string {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	if extraEnv != nil {
		cmd.Env = append(os.Environ(), extraEnv...)
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v in %s failed: %v: %s", name, args, dir, err, out)
	}
	return string(out)
}
