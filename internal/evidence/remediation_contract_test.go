package evidence_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/douglasjarquin/made/internal/evidence"
)

func TestInRepoStore_RejectsPathTraversalAndOversizedEvidence(t *testing.T) {
	store := &evidence.InRepoStore{RepoPath: t.TempDir(), Dir: ".made/evidence"}

	if err := store.WriteEvidence("run-1", map[string][]byte{
		"../escaped.txt": []byte("must stay inside the run"),
	}); err == nil {
		t.Fatal("evidence store accepted a path traversal outside the run evidence directory")
	}

	if err := store.WriteEvidence("run-1", map[string][]byte{
		"large.log": []byte(strings.Repeat("x", 2<<20)),
	}); err == nil {
		t.Fatal("evidence store accepted output beyond the bounded retention limit")
	}
}

func TestNewStoreAppliesConfiguredRetentionBound(t *testing.T) {
	store, ok := evidence.NewStore(t.TempDir(), evidence.Config{StoreInRepo: true, RetentionBytes: 8}).(*evidence.InRepoStore)
	if !ok {
		t.Fatal("NewStore returned the wrong store type")
	}
	if store.RetentionBytes != 8 {
		t.Fatalf("retention bytes = %d, want 8", store.RetentionBytes)
	}
	if err := store.WriteEvidence("run-1", map[string][]byte{"log.txt": []byte("123456789")}); err == nil {
		t.Fatal("configured evidence retention bound was not enforced")
	}
}

func TestInRepoStore_RedactsPublishedSecrets(t *testing.T) {
	repo := t.TempDir()
	store := &evidence.InRepoStore{RepoPath: repo, Dir: ".made/evidence"}
	input := "Authorization: Bearer bearer-secret\napi_key=api-secret\n\"access_token\": \"json-secret\"\nx-api-key: header-secret\ntoken=query-secret&ok=1\nAWS_SECRET_ACCESS_KEY=aws-secret\nOPENAI_API_KEY=openai-secret\nDATABASE_URL=postgres://user:password@db.example/app\nghp_1234567890abcdef\nAKIA1234567890ABCDEF\n-----BEGIN RSA PRIVATE KEY-----\nprivate-secret\n-----END RSA PRIVATE KEY-----\n"
	if err := store.WriteEvidence("run-1", map[string][]byte{"log.txt": []byte(input)}); err != nil {
		t.Fatalf("WriteEvidence: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(repo, ".made/evidence", "run-1", "log.txt"))
	if err != nil {
		t.Fatalf("read evidence: %v", err)
	}
	for _, secret := range []string{"bearer-secret", "api-secret", "json-secret", "header-secret", "query-secret", "aws-secret", "openai-secret", "postgres://user:password@db.example/app", "ghp_1234567890abcdef", "AKIA1234567890ABCDEF", "private-secret"} {
		if strings.Contains(string(data), secret) {
			t.Fatalf("published evidence retained %q: %q", secret, data)
		}
	}
}

func TestInRepoStore_PublishesEvidenceInAccessibleCommit(t *testing.T) {
	repo := t.TempDir()
	runEvidenceGit(t, repo, "init", "-q", "-b", "main")
	runEvidenceGit(t, repo, "config", "user.name", "evidence-test")
	runEvidenceGit(t, repo, "config", "user.email", "evidence-test@example.com")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("fixture\n"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	runEvidenceGit(t, repo, "add", "README.md")
	runEvidenceGit(t, repo, "commit", "-q", "-m", "fixture")

	store := &evidence.InRepoStore{RepoPath: repo, Dir: ".made/evidence"}
	if err := store.WriteEvidence("run-1", map[string][]byte{"log.txt": []byte("visible evidence\n")}); err != nil {
		t.Fatalf("WriteEvidence: %v", err)
	}
	if err := store.PublishEvidence("run-1"); err != nil {
		t.Fatalf("PublishEvidence: %v", err)
	}
	if got := strings.TrimSpace(string(runEvidenceGit(t, repo, "show", "--format=", "--name-only", "HEAD"))); !strings.Contains(got, ".made/evidence/run-1/log.txt") {
		t.Fatalf("published commit does not contain evidence path: %q", got)
	}
}

func TestInRepoStore_PublishSuppressesRepositoryHooksAndAmbientGitConfig(t *testing.T) {
	repo := initTargetRepo(t)
	store := &evidence.InRepoStore{RepoPath: repo, Dir: ".made/evidence"}
	if err := store.WriteEvidence("run-hooks", map[string][]byte{"log.txt": []byte("visible evidence\n")}); err != nil {
		t.Fatalf("WriteEvidence: %v", err)
	}
	hooksDir := t.TempDir()
	hookMarker := filepath.Join(t.TempDir(), "hook-fired")
	if err := os.WriteFile(filepath.Join(hooksDir, "pre-commit"), []byte("#!/bin/sh\nprintf fired > '"+hookMarker+"'\n"), 0o700); err != nil {
		t.Fatalf("write pre-commit hook: %v", err)
	}
	t.Setenv("GIT_CONFIG_COUNT", "2")
	t.Setenv("GIT_CONFIG_KEY_0", "core.hooksPath")
	t.Setenv("GIT_CONFIG_VALUE_0", hooksDir)
	t.Setenv("GIT_CONFIG_KEY_1", "commit.gpgsign")
	t.Setenv("GIT_CONFIG_VALUE_1", "false")

	if err := store.PublishEvidence("run-hooks"); err != nil {
		t.Fatalf("PublishEvidence: %v", err)
	}
	if _, err := os.Stat(hookMarker); !os.IsNotExist(err) {
		t.Fatalf("evidence publication executed an inherited Git hook: %v", err)
	}
}

func TestInRepoStore_PublishRejectsSymlinkedEvidenceFile(t *testing.T) {
	repo := initTargetRepo(t)
	store := &evidence.InRepoStore{RepoPath: repo, Dir: ".made/evidence"}
	if err := store.WriteEvidence("run-symlink", map[string][]byte{"safe.log": []byte("safe\n")}); err != nil {
		t.Fatalf("WriteEvidence: %v", err)
	}
	outside := filepath.Join(t.TempDir(), "outside.log")
	if err := os.WriteFile(outside, []byte("token=outside-secret\n"), 0o600); err != nil {
		t.Fatalf("write outside file: %v", err)
	}
	leakPath := filepath.Join(repo, ".made", "evidence", "run-symlink", "leak.log")
	if err := os.Symlink(outside, leakPath); err != nil {
		t.Fatalf("create evidence symlink: %v", err)
	}

	if err := store.PublishEvidence("run-symlink"); err == nil {
		t.Fatal("PublishEvidence followed or published a symlinked evidence file")
	}
	if _, err := os.Stat(outside); err != nil {
		t.Fatalf("outside evidence target disappeared: %v", err)
	}
}

func TestInRepoStore_PublishRedactsInjectedEvidence(t *testing.T) {
	repo := initTargetRepo(t)
	store := &evidence.InRepoStore{RepoPath: repo, Dir: ".made/evidence", RetentionBytes: 128}
	if err := store.WriteEvidence("run-injected", map[string][]byte{"safe.log": []byte("safe\n")}); err != nil {
		t.Fatalf("WriteEvidence: %v", err)
	}
	injectedPath := filepath.Join(repo, ".made", "evidence", "run-injected", "injected.log")
	if err := os.WriteFile(injectedPath, []byte("token=injected-secret\n"), 0o600); err != nil {
		t.Fatalf("write injected evidence: %v", err)
	}

	if err := store.PublishEvidence("run-injected"); err != nil {
		t.Fatalf("PublishEvidence: %v", err)
	}
	data := run(t, repo, "git", "show", "HEAD:.made/evidence/run-injected/injected.log")
	if strings.Contains(data, "injected-secret") {
		t.Fatalf("published injected evidence retained secret: %q", data)
	}
}

func TestInRepoStore_PublishRejectsOversizedInjectedEvidence(t *testing.T) {
	repo := initTargetRepo(t)
	store := &evidence.InRepoStore{RepoPath: repo, Dir: ".made/evidence", RetentionBytes: 16}
	if err := store.WriteEvidence("run-large", map[string][]byte{"safe.log": []byte("safe\n")}); err != nil {
		t.Fatalf("WriteEvidence: %v", err)
	}
	largePath := filepath.Join(repo, ".made", "evidence", "run-large", "large.log")
	if err := os.WriteFile(largePath, []byte(strings.Repeat("x", 17)), 0o600); err != nil {
		t.Fatalf("write oversized evidence: %v", err)
	}

	if err := store.PublishEvidence("run-large"); err == nil {
		t.Fatal("PublishEvidence accepted evidence beyond the configured retention bound")
	}
}

func runEvidenceGit(t *testing.T, dir string, args ...string) []byte {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return out
}

func TestInRepoStore_UsesPrivateEvidencePermissions(t *testing.T) {
	repo := t.TempDir()
	store := &evidence.InRepoStore{RepoPath: repo, Dir: ".made/evidence"}
	if err := os.MkdirAll(filepath.Join(repo, ".made", "evidence", "run-1"), 0o755); err != nil {
		t.Fatalf("create existing evidence directories: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".made", "evidence", "run-1", "log.txt"), []byte("old"), 0o644); err != nil {
		t.Fatalf("create existing evidence file: %v", err)
	}
	if err := store.WriteEvidence("run-1", map[string][]byte{"log.txt": []byte("bounded")}); err != nil {
		t.Fatalf("WriteEvidence: %v", err)
	}
	for _, tc := range []struct {
		name string
		want os.FileMode
	}{
		{name: ".made/evidence", want: 0o700},
		{name: ".made/evidence/run-1", want: 0o700},
		{name: ".made/evidence/run-1/log.txt", want: 0o600},
	} {
		info, err := os.Stat(filepath.Join(repo, tc.name))
		if err != nil {
			t.Fatalf("stat %s: %v", tc.name, err)
		}
		if got := info.Mode().Perm(); got != tc.want {
			t.Errorf("permissions for %s = %o, want %o", tc.name, got, tc.want)
		}
	}
}

func TestInRepoStore_RejectsSymlinkedEvidenceDirectory(t *testing.T) {
	repo := t.TempDir()
	outside := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".made"), 0o755); err != nil {
		t.Fatalf("create evidence parent: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(repo, ".made", "evidence")); err != nil {
		t.Fatalf("create evidence symlink: %v", err)
	}
	store := &evidence.InRepoStore{RepoPath: repo, Dir: ".made/evidence"}
	if err := store.WriteEvidence("run-1", map[string][]byte{"log.txt": []byte("must remain inside")}); err == nil {
		t.Fatal("evidence store followed a symlinked evidence directory")
	}
	entries, err := os.ReadDir(outside)
	if err != nil {
		t.Fatalf("read outside directory: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("evidence escaped through symlink: %+v", entries)
	}
}

func TestInRepoStore_PublishRejectsSymlinkedEvidenceRoot(t *testing.T) {
	repo := initTargetRepo(t)
	outside := t.TempDir()
	if err := os.MkdirAll(filepath.Join(outside, "run-escape"), 0o700); err != nil {
		t.Fatalf("create outside run: %v", err)
	}
	if err := os.WriteFile(filepath.Join(outside, "run-escape", "leak.log"), []byte("token=outside-secret\n"), 0o600); err != nil {
		t.Fatalf("write outside evidence: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(repo, ".made"), 0o700); err != nil {
		t.Fatalf("create evidence parent: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(repo, ".made", "evidence")); err != nil {
		t.Fatalf("create evidence root symlink: %v", err)
	}

	store := &evidence.InRepoStore{RepoPath: repo, Dir: ".made/evidence"}
	if err := store.PublishEvidence("run-escape"); err == nil {
		t.Fatal("PublishEvidence followed a symlinked evidence root")
	}
}
