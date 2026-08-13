package evidence_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func initTargetRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run(t, dir, "git", "init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("target repo\n"), 0o644); err != nil {
		t.Fatalf("write fixture file: %v", err)
	}
	run(t, dir, "git", "add", "README.md")
	runEnv(t, dir, commitEnv(), "git", "commit", "-q", "-m", "initial commit")
	return dir
}

func commitEnv() []string {
	return []string{
		"GIT_AUTHOR_NAME=evidence-test",
		"GIT_AUTHOR_EMAIL=evidence-test@example.com",
		"GIT_COMMITTER_NAME=evidence-test",
		"GIT_COMMITTER_EMAIL=evidence-test@example.com",
	}
}

func run(t *testing.T, dir string, name string, args ...string) string {
	t.Helper()
	return runEnv(t, dir, nil, name, args...)
}

func runEnv(t *testing.T, dir string, extraEnv []string, name string, args ...string) string {
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

func runNoFatal(dir string, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}
