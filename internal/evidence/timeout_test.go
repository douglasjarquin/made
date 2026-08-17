package evidence

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestInRepoStore_PublishHonorsGitTimeout(t *testing.T) {
	repo := t.TempDir()
	runEvidenceGitTest(t, repo, "init", "-q", "-b", "main")
	runEvidenceGitTest(t, repo, "config", "user.name", "evidence-test")
	runEvidenceGitTest(t, repo, "config", "user.email", "evidence-test@example.com")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("fixture\n"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	runEvidenceGitTest(t, repo, "add", "README.md")
	runEvidenceGitTest(t, repo, "commit", "-q", "-m", "fixture")

	store := &InRepoStore{RepoPath: repo, Dir: ".made/evidence"}
	if err := store.WriteEvidence("run-timeout", map[string][]byte{"log.txt": []byte("evidence\n")}); err != nil {
		t.Fatalf("WriteEvidence: %v", err)
	}
	hookDir := filepath.Join(repo, ".git", "hooks")
	if err := os.WriteFile(filepath.Join(hookDir, "pre-commit"), []byte("#!/bin/sh\nsleep 5\n"), 0o700); err != nil {
		t.Fatalf("write blocking hook: %v", err)
	}

	originalTimeout := evidenceGitTimeout
	evidenceGitTimeout = 50 * time.Millisecond
	t.Cleanup(func() { evidenceGitTimeout = originalTimeout })
	started := time.Now()
	err := store.PublishEvidenceContext(context.Background(), "run-timeout")
	if err == nil {
		t.Fatal("PublishEvidenceContext returned nil despite a blocking git hook")
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("PublishEvidenceContext exceeded bounded timeout: %s", elapsed)
	}
}

func runEvidenceGitTest(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_GLOBAL=/dev/null",
		"SSH_AUTH_SOCK=",
		"GIT_AUTHOR_NAME=evidence-test",
		"GIT_AUTHOR_EMAIL=evidence-test@example.com",
		"GIT_COMMITTER_NAME=evidence-test",
		"GIT_COMMITTER_EMAIL=evidence-test@example.com",
	)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, strings.TrimSpace(string(output)))
	}
}
