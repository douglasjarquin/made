package orchestrator

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/douglasjarquin/made/internal/gitgate"
)

func TestSetupResolvesTrustedConfigWhenPresentOnDefaultBranch(t *testing.T) {
	dir := t.TempDir()
	barePath := filepath.Join(dir, "gate.git")
	if err := gitgate.InitBare(barePath); err != nil {
		t.Fatalf("InitBare: %v", err)
	}
	runGit(t, barePath, "remote", "add", "origin", barePath)

	src := filepath.Join(dir, "src")
	initSourceRepo(t, src)
	writeFile(t, src, ".made.yml", "version: 1\nno_ci: true\nallow_repo_commands: true\ncommands:\n  test: \"go test ./...\"\n")
	commit(t, src, "add made.yml")
	sha := pushBranch(t, src, barePath, "main")

	worktreesDir := filepath.Join(dir, "worktrees")
	rc, err := Setup(context.Background(), barePath, "main", worktreesDir, "run-1", sha)
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}
	defer rc.Cleanup(context.Background())

	if !rc.Config.NoCI {
		t.Fatalf("expected NoCI true from trusted config, got %+v", rc.Config)
	}
	if !rc.Config.AllowRepoCommands {
		t.Fatalf("expected AllowRepoCommands true from trusted config, got %+v", rc.Config)
	}
	if rc.Config.Commands.Test != "go test ./..." {
		t.Fatalf("expected trusted test command, got %q", rc.Config.Commands.Test)
	}
}

func TestSetupBootstrapsFromPushedMadeYmlWhenTrustedCopyAbsent(t *testing.T) {
	dir := t.TempDir()
	barePath := filepath.Join(dir, "gate.git")
	if err := gitgate.InitBare(barePath); err != nil {
		t.Fatalf("InitBare: %v", err)
	}
	runGit(t, barePath, "remote", "add", "origin", barePath)

	src := filepath.Join(dir, "src")
	initSourceRepo(t, src)
	mainSHA := pushBranch(t, src, barePath, "main")

	writeFile(t, src, ".made.yml", "version: 1\nno_ci: true\ncommands:\n  test: \"go test ./...\"\n")
	commit(t, src, "add made.yml on feature branch")
	featureSHA := pushBranch(t, src, barePath, "feature")

	worktreesDir := filepath.Join(dir, "worktrees")
	rc, err := Setup(context.Background(), barePath, "main", worktreesDir, "run-bootstrap", featureSHA)
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}
	defer rc.Cleanup(context.Background())

	if !rc.Config.NoCI {
		t.Fatalf("expected NoCI true from bootstrapped pushed config, got %+v", rc.Config)
	}
	if rc.Config.Commands.Test != "go test ./..." {
		t.Fatalf("expected bootstrapped test command, got %q", rc.Config.Commands.Test)
	}
	if revParse(t, rc.Worktree.Path, "HEAD") != featureSHA {
		t.Fatalf("worktree HEAD = %s, want feature SHA %s (main tip is %s)", revParse(t, rc.Worktree.Path, "HEAD"), featureSHA, mainSHA)
	}
}

func TestSetupResolvesEmptyTrustedConfigWhenMadeYmlMissingFromDefaultBranch(t *testing.T) {
	dir := t.TempDir()
	barePath := filepath.Join(dir, "gate.git")
	if err := gitgate.InitBare(barePath); err != nil {
		t.Fatalf("InitBare: %v", err)
	}
	runGit(t, barePath, "remote", "add", "origin", barePath)

	src := filepath.Join(dir, "src")
	initSourceRepo(t, src)
	sha := pushBranch(t, src, barePath, "main")

	worktreesDir := filepath.Join(dir, "worktrees")
	rc, err := Setup(context.Background(), barePath, "main", worktreesDir, "run-2", sha)
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}
	defer rc.Cleanup(context.Background())

	if rc.Config.Agent != "" || rc.Config.Commands.Test != "" || rc.Config.Commands.Lint != "" {
		t.Fatalf("expected empty executable fields with no trusted copy, got %+v", rc.Config)
	}
}

func TestSetupResolvesEmptyTrustedConfigWhenDefaultBranchNeverFetched(t *testing.T) {
	dir := t.TempDir()
	barePath := filepath.Join(dir, "gate.git")
	if err := gitgate.InitBare(barePath); err != nil {
		t.Fatalf("InitBare: %v", err)
	}
	runGit(t, barePath, "remote", "add", "origin", barePath)

	src := filepath.Join(dir, "src")
	initSourceRepo(t, src)
	sha := pushBranch(t, src, barePath, "feature")

	worktreesDir := filepath.Join(dir, "worktrees")
	rc, err := Setup(context.Background(), barePath, "main", worktreesDir, "run-3", sha)
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}
	defer rc.Cleanup(context.Background())

	if rc.Config.Agent != "" || rc.Config.Commands.Test != "" {
		t.Fatalf("expected empty executable fields when default branch never fetched, got %+v", rc.Config)
	}
}

func TestRefreshDefaultBranchClearsDeletedRemotePolicyRef(t *testing.T) {
	dir := t.TempDir()
	gatePath := filepath.Join(dir, "gate.git")
	remotePath := filepath.Join(dir, "remote.git")
	runGit(t, "", "init", "--bare", "-q", "-b", "main", remotePath)
	if err := gitgate.InitBare(gatePath); err != nil {
		t.Fatalf("InitBare gate: %v", err)
	}
	runGit(t, gatePath, "remote", "add", "origin", remotePath)

	src := filepath.Join(dir, "src")
	initSourceRepo(t, src)
	sha := pushBranch(t, src, remotePath, "main")
	if err := refreshDefaultBranch(context.Background(), gatePath, "main"); err != nil {
		t.Fatalf("initial refreshDefaultBranch: %v", err)
	}
	if got := revParse(t, gatePath, "refs/heads/main"); got != sha {
		t.Fatalf("fetched trusted ref = %s, want %s", got, sha)
	}
	runGit(t, remotePath, "update-ref", "-d", "refs/heads/main")

	if err := refreshDefaultBranch(context.Background(), gatePath, "main"); err != nil {
		t.Fatalf("refreshDefaultBranch after remote deletion: %v", err)
	}
	cmd := exec.Command("git", "rev-parse", "--verify", "refs/heads/main")
	cmd.Dir = gatePath
	if err := cmd.Run(); err == nil {
		t.Fatal("refreshDefaultBranch retained a deleted remote trusted ref")
	}
}

func TestSetupCutsWorktreeAtExactPushedSHANotBranchTip(t *testing.T) {
	dir := t.TempDir()
	barePath := filepath.Join(dir, "gate.git")
	if err := gitgate.InitBare(barePath); err != nil {
		t.Fatalf("InitBare: %v", err)
	}
	runGit(t, barePath, "remote", "add", "origin", barePath)

	src := filepath.Join(dir, "src")
	initSourceRepo(t, src)
	writeFile(t, src, "marker.txt", "commit-a\n")
	commit(t, src, "commit A")
	shaA := revParse(t, src, "HEAD")
	pushBranch(t, src, barePath, "main")

	writeFile(t, src, "marker.txt", "commit-b\n")
	commit(t, src, "commit B")
	shaB := pushBranch(t, src, barePath, "main")
	if shaA == shaB {
		t.Fatalf("test setup bug: expected distinct commits, got %s twice", shaA)
	}

	worktreesDir := filepath.Join(dir, "worktrees")
	rc, err := Setup(context.Background(), barePath, "main", worktreesDir, "run-4", shaA)
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}
	defer rc.Cleanup(context.Background())

	gotHead := revParse(t, rc.Worktree.Path, "HEAD")
	if gotHead != shaA {
		t.Fatalf("worktree HEAD = %s, want exact pushed SHA %s (branch tip is %s)", gotHead, shaA, shaB)
	}
	got, err := os.ReadFile(filepath.Join(rc.Worktree.Path, "marker.txt"))
	if err != nil {
		t.Fatalf("read marker.txt: %v", err)
	}
	if string(got) != "commit-a\n" {
		t.Fatalf("marker.txt = %q, want content from commit A, not the branch tip", got)
	}
}

func TestSetupRecoversFromMidSetupPanicAndCleansUp(t *testing.T) {
	dir := t.TempDir()
	barePath := filepath.Join(dir, "gate.git")
	if err := gitgate.InitBare(barePath); err != nil {
		t.Fatalf("InitBare: %v", err)
	}
	runGit(t, barePath, "remote", "add", "origin", barePath)

	src := filepath.Join(dir, "src")
	initSourceRepo(t, src)
	sha := pushBranch(t, src, barePath, "main")

	worktreesDir := filepath.Join(dir, "worktrees")

	old := setupTestHook
	setupTestHook = func() { panic("simulated mid-setup panic") }
	defer func() { setupTestHook = old }()

	rc, err := Setup(context.Background(), barePath, "main", worktreesDir, "run-5", sha)
	if err == nil {
		t.Fatal("expected recovered panic to surface as an error")
	}
	if rc != nil {
		t.Fatalf("expected nil RunContext on recovered panic, got %+v", rc)
	}

	entries, rdErr := os.ReadDir(worktreesDir)
	if rdErr != nil && !os.IsNotExist(rdErr) {
		t.Fatalf("read worktrees dir: %v", rdErr)
	}
	if len(entries) != 0 {
		t.Fatalf("expected zero leftover worktree dirs after recovered panic, found %d: %v", len(entries), entries)
	}
}

func TestRunWrapsSetupWorkAndCleanupWithPanicRecovery(t *testing.T) {
	dir := t.TempDir()
	barePath := filepath.Join(dir, "gate.git")
	if err := gitgate.InitBare(barePath); err != nil {
		t.Fatalf("InitBare: %v", err)
	}
	runGit(t, barePath, "remote", "add", "origin", barePath)

	src := filepath.Join(dir, "src")
	initSourceRepo(t, src)
	sha := pushBranch(t, src, barePath, "main")

	worktreesDir := filepath.Join(dir, "worktrees")

	err := Run(context.Background(), barePath, "main", worktreesDir, "run-6", sha, func(ctx context.Context, rc *RunContext) error {
		panic("simulated panic during caller-provided work")
	})
	if err == nil {
		t.Fatal("expected Run to convert the panic into an error")
	}

	entries, rdErr := os.ReadDir(worktreesDir)
	if rdErr != nil && !os.IsNotExist(rdErr) {
		t.Fatalf("read worktrees dir: %v", rdErr)
	}
	if len(entries) != 0 {
		t.Fatalf("expected zero leftover worktree dirs after Run recovers a panic, found %d: %v", len(entries), entries)
	}
}

func initSourceRepo(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir source repo: %v", err)
	}
	runGit(t, dir, "init", "-q", "-b", "main")
	writeFile(t, dir, "README.md", "orchestrator fixture\n")
	runGit(t, dir, "add", "README.md")
	commit(t, dir, "initial commit")
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

func commit(t *testing.T, dir, message string) {
	t.Helper()
	runGit(t, dir, "add", "-A")
	cmd := exec.Command("git", "commit", "-q", "-m", message)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=orchestrator-test",
		"GIT_AUTHOR_EMAIL=orchestrator-test@example.com",
		"GIT_COMMITTER_NAME=orchestrator-test",
		"GIT_COMMITTER_EMAIL=orchestrator-test@example.com",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git commit in %s failed: %v: %s", dir, err, out)
	}
}

func pushBranch(t *testing.T, srcDir, barePath, branch string) string {
	t.Helper()
	cmd := exec.Command("git", "push", barePath, "HEAD:refs/heads/"+branch)
	cmd.Dir = srcDir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git push %s in %s failed: %v: %s", branch, srcDir, err, out)
	}
	return revParse(t, srcDir, "HEAD")
}

func revParse(t *testing.T, dir, ref string) string {
	t.Helper()
	cmd := exec.Command("git", "rev-parse", ref)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git rev-parse %s in %s failed: %v: %s", ref, dir, err, out)
	}
	return trimNewline(string(out))
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v in %s failed: %v: %s", args, dir, err, out)
	}
}

func trimNewline(s string) string {
	for len(s) > 0 && (s[len(s)-1] == '\n' || s[len(s)-1] == '\r') {
		s = s[:len(s)-1]
	}
	return s
}
