package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/douglasjarquin/made/internal/gitgate"
)

func TestGateInit_EndToEnd(t *testing.T) {
	home := shortTempDir(t)
	t.Setenv("MADE_HOME", home)

	scratch := shortTempDir(t)
	remoteDir := filepath.Join(scratch, "remote.git")
	sourceDir := filepath.Join(scratch, "source")
	remoteURL := "file://" + remoteDir

	testGit(t, "", "init", "--bare", "-b", "main", remoteDir)
	testGit(t, "", "init", "-b", "main", sourceDir)
	writeAndCommit(t, sourceDir, "README.md", "hello\n", "init")
	testGit(t, sourceDir, "remote", "add", "origin", remoteURL)
	testGit(t, sourceDir, "push", "origin", "main")

	wantTip := strings.TrimSpace(testGitOutput(t, sourceDir, "rev-parse", "main"))

	out, errOut, code := runCapture(t, []string{"gate", "init", sourceDir, remoteURL})
	if code != 0 {
		t.Fatalf("gate init exit code = %d, want 0; stdout=%s stderr=%s", code, out, errOut)
	}

	barePath := gitgate.GatePath(home, resolvedPath(t, sourceDir))

	if info, err := os.Stat(barePath); err != nil || !info.IsDir() {
		t.Fatalf("expected bare gate repo at %s, stat err=%v", barePath, err)
	}

	if got := strings.TrimSpace(testGitOutput(t, barePath, "remote", "get-url", "origin")); got != remoteURL {
		t.Fatalf("origin remote = %q, want %q", got, remoteURL)
	}

	if got := strings.TrimSpace(testGitOutput(t, barePath, "config", "made.real-remote")); got != remoteURL {
		t.Fatalf("made.real-remote = %q, want %q", got, remoteURL)
	}

	if got := strings.TrimSpace(testGitOutput(t, barePath, "rev-parse", "refs/heads/main")); got != wantTip {
		t.Fatalf("refs/heads/main tip = %q, want %q", got, wantTip)
	}

	for _, hook := range []string{"pre-receive", "post-receive"} {
		path := filepath.Join(barePath, "hooks", hook)
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("expected hook %s installed: %v", hook, err)
		}
		if info.Mode()&0o100 == 0 {
			t.Fatalf("expected hook %s to be executable, mode=%v", hook, info.Mode())
		}
	}

	if got := strings.TrimSpace(testGitOutput(t, sourceDir, "remote", "get-url", "made")); got != barePath {
		t.Fatalf("made remote in target repo = %q, want %q", got, barePath)
	}
}

func TestGateInit_ReInitIsIdempotent(t *testing.T) {
	home := shortTempDir(t)
	t.Setenv("MADE_HOME", home)

	scratch := shortTempDir(t)
	remoteDir := filepath.Join(scratch, "remote.git")
	sourceDir := filepath.Join(scratch, "source")
	remoteURL := "file://" + remoteDir

	testGit(t, "", "init", "--bare", "-b", "main", remoteDir)
	testGit(t, "", "init", "-b", "main", sourceDir)
	writeAndCommit(t, sourceDir, "README.md", "hello\n", "init")
	testGit(t, sourceDir, "remote", "add", "origin", remoteURL)
	testGit(t, sourceDir, "push", "origin", "main")

	_, errOut1, code1 := runCapture(t, []string{"gate", "init", sourceDir, remoteURL})
	if code1 != 0 {
		t.Fatalf("first gate init exit code = %d; stderr=%s", code1, errOut1)
	}

	writeAndCommit(t, sourceDir, "second.txt", "more\n", "second commit")
	testGit(t, sourceDir, "push", "origin", "main")
	wantTip := strings.TrimSpace(testGitOutput(t, sourceDir, "rev-parse", "main"))

	out2, errOut2, code2 := runCapture(t, []string{"gate", "init", sourceDir, remoteURL})
	if code2 != 0 {
		t.Fatalf("second gate init exit code = %d, want 0 (re-init must be safe); stdout=%s stderr=%s", code2, out2, errOut2)
	}

	barePath := gitgate.GatePath(home, resolvedPath(t, sourceDir))

	if got := strings.TrimSpace(testGitOutput(t, barePath, "rev-parse", "refs/heads/main")); got != wantTip {
		t.Fatalf("expected re-init to re-fetch the updated default branch tip, got %q, want %q", got, wantTip)
	}

	remotes := strings.Fields(testGitOutput(t, barePath, "remote"))
	if n := countOccurrences(remotes, "origin"); n != 1 {
		t.Fatalf("expected exactly one origin remote in bare repo after re-init, got %d in %v", n, remotes)
	}

	targetRemotes := strings.Fields(testGitOutput(t, sourceDir, "remote"))
	if n := countOccurrences(targetRemotes, "made"); n != 1 {
		t.Fatalf("expected exactly one made remote in target repo after re-init, got %d in %v", n, targetRemotes)
	}
}

func countOccurrences(items []string, target string) int {
	n := 0
	for _, item := range items {
		if item == target {
			n++
		}
	}
	return n
}

func resolvedPath(t *testing.T, path string) string {
	t.Helper()
	abs, err := filepath.Abs(path)
	if err != nil {
		t.Fatalf("filepath.Abs(%s): %v", path, err)
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return abs
	}
	return resolved
}

func writeAndCommit(t *testing.T, repoDir, name, content, message string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(repoDir, name), []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	testGit(t, repoDir, "add", ".")
	testGit(t, repoDir, "-c", "user.email=test@example.com", "-c", "user.name=Test", "commit", "-m", message)
}

func testGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_COUNT=2",
		"GIT_CONFIG_KEY_0=commit.gpgsign",
		"GIT_CONFIG_VALUE_0=false",
		"GIT_CONFIG_KEY_1=safe.bareRepository",
		"GIT_CONFIG_VALUE_1=all",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s (dir=%s): %v: %s", strings.Join(args, " "), dir, err, out)
	}
}

func testGitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_COUNT=2",
		"GIT_CONFIG_KEY_0=commit.gpgsign",
		"GIT_CONFIG_VALUE_0=false",
		"GIT_CONFIG_KEY_1=safe.bareRepository",
		"GIT_CONFIG_VALUE_1=all",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s (dir=%s): %v: %s", strings.Join(args, " "), dir, err, out)
	}
	return string(out)
}
