package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/douglasjarquin/made/internal/api"
	"github.com/douglasjarquin/made/internal/config"
	"github.com/douglasjarquin/made/internal/managed"
	"github.com/douglasjarquin/made/internal/planner"
	"github.com/douglasjarquin/made/internal/receipt"
)

const lanereuseSafetyConfig = `version: 1
validation:
  lanes:
    go:
      paths: ["**/*.go"]
      full: ["echo go-full"]
      required_before_push: true
`

// newLaneReuseSafetyRepo builds a verify-ready repo with a real bare
// "origin" remote (unlike newVerifyTestRepo's faked-ref-only setup), because
// the safety property under test is specifically about what made verify
// does or does not push to that remote.
func newLaneReuseSafetyRepo(t *testing.T) (dir, remote string) {
	t.Helper()
	dir = shortTempDir(t)
	remoteParent := shortTempDir(t)
	remote = filepath.Join(remoteParent, "remote.git")

	gitVerifyAt(t, remoteParent, "init", "-q", "--bare", remote)
	gitVerifyAt(t, dir, "init", "-b", "main")
	gitVerifyAt(t, dir, "config", "user.email", "test@test.local")
	gitVerifyAt(t, dir, "config", "user.name", "test")
	gitVerifyAt(t, dir, "remote", "add", "origin", remote)

	if err := os.WriteFile(filepath.Join(dir, ".made.yaml"), []byte(lanereuseSafetyConfig), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitVerifyAt(t, dir, "add", ".")
	gitVerifyAt(t, dir, "commit", "-m", "initial")
	baseSHA := gitVerifyAt(t, dir, "rev-parse", "HEAD")
	gitVerifyAt(t, dir, "update-ref", "refs/remotes/origin/main", baseSHA)

	if err := os.WriteFile(filepath.Join(dir, "hello.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitVerifyAt(t, dir, "add", ".")
	gitVerifyAt(t, dir, "commit", "-m", "add hello.go")

	return dir, remote
}

// publishSafetyTestReceipt publishes a receipt for the "go" lane's Full
// command directly via internal/receipt.Store.Put, from test setup only -
// this is the one place in the whole test suite allowed to call Put, so the
// scenario below tests a genuine reuse hit without production code ever
// calling it itself.
func publishSafetyTestReceipt(t *testing.T, dir string) {
	t.Helper()
	baseSHA := gitVerifyAt(t, dir, "rev-parse", "origin/main")
	inputSHA := gitVerifyAt(t, dir, "rev-parse", "HEAD")

	configBytes, err := os.ReadFile(filepath.Join(dir, ".made.yaml"))
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	cfg, err := config.ParseBytes(configBytes)
	if err != nil {
		t.Fatalf("parse config: %v", err)
	}
	decisions, err := planner.SelectLanes(cfg.Validation.Lanes, []string{"hello.go"})
	if err != nil {
		t.Fatalf("SelectLanes: %v", err)
	}
	configHash, err := planner.HashConfig(cfg)
	if err != nil {
		t.Fatalf("HashConfig: %v", err)
	}
	var matchedPaths []string
	for _, d := range decisions {
		if d.Name == "go" {
			matchedPaths = d.MatchedPaths
		}
	}
	fp := receipt.BuildLaneFingerprint(receipt.LaneFingerprintInputs{
		RepoIdentity: receipt.RepoIdentity(context.Background(), dir),
		BaseSHA:      baseSHA,
		CandidateSHA: inputSHA,
		ConfigHash:   configHash,
		LaneName:     "go",
		MatchedPaths: matchedPaths,
		Command:      cfg.Validation.Lanes["go"].FullShellCommands()[0],
		MadeVersion:  managed.MadeVersion,
	})
	store := &receipt.Store{RepoPath: dir}
	now := time.Now().UTC()
	if _, err := store.Put(context.Background(), fp.Hash(), receipt.Receipt{
		SchemaVersion: receipt.ReceiptSchemaVersion,
		Fingerprint:   fp,
		SourceRunID:   "prior-run",
		StartedAt:     now,
		CompletedAt:   now,
		MadeVersion:   managed.MadeVersion,
	}); err != nil {
		t.Fatalf("Put: %v", err)
	}
}

// writeGitSpy installs a "git" wrapper ahead of the real git on PATH that
// appends every invocation's argv to logPath and then execs the real git
// binary, so the wrapped commands still function - the point is to observe
// every git subcommand made verify runs, not to change any of them.
func writeGitSpy(t *testing.T, binDir, logPath string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("git spy shell script requires a POSIX shell")
	}
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Fatalf("locate real git: %v", err)
	}
	script := "#!/bin/sh\necho \"$*\" >> " + shellQuote(logPath) + "\nexec " + shellQuote(realGit) + " \"$@\"\n"
	if err := os.WriteFile(filepath.Join(binDir, "git"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func gitInvocationsContainPush(t *testing.T, logPath string) bool {
	t.Helper()
	data, err := os.ReadFile(logPath)
	if err != nil {
		if os.IsNotExist(err) {
			return false
		}
		t.Fatalf("read git spy log: %v", err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) > 0 && fields[0] == "push" {
			return true
		}
	}
	return false
}

// TestVerifyRun_NeverPushesOrPublishesReceipts is the single most
// safety-critical test in this change: internal/managed and internal/verify
// must never call receipt.Store.Put or invoke `git push`, in either a fresh
// cache-miss run or a real reuse hit - reuse is read-only and opportunistic
// (project issue #61); publishing remains exclusively the daemon-backed
// pipeline's job.
func TestVerifyRun_NeverPushesOrPublishesReceipts(t *testing.T) {
	dir, remote := newLaneReuseSafetyRepo(t)

	spyBinDir := shortTempDir(t)
	logPath := filepath.Join(shortTempDir(t), "git-invocations.log")
	writeGitSpy(t, spyBinDir, logPath)
	t.Setenv("PATH", spyBinDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	madeHomeDir := shortTempDir(t)
	t.Setenv("MADE_HOME", madeHomeDir)

	beforeRefs := gitVerifyAt(t, remote, "for-each-ref")

	// Fresh cache-miss: no receipt exists yet, so the go lane's Full command
	// must actually run.
	if _, stderr, code := runCapture(t, []string{"verify", "run", "--repo", dir, "--base-ref", "origin/main", "--json"}); code != 0 {
		t.Fatalf("verify run (cache miss): exit %d stderr=%s", code, stderr)
	}
	if gitInvocationsContainPush(t, logPath) {
		t.Fatal("expected no `git push` on a fresh cache-miss run")
	}

	// A real reuse hit: a receipt was published (simulating the daemon-backed
	// pipeline having already run this), so the go lane's Full command must
	// be satisfied by Store.Get instead of running - and must still never
	// push anything.
	publishSafetyTestReceipt(t, dir)
	afterSetupPublishRefs := gitVerifyAt(t, remote, "for-each-ref")

	stdout, stderr, code := runCapture(t, []string{"verify", "run", "--repo", dir, "--base-ref", "origin/main", "--json"})
	if code != 0 {
		t.Fatalf("verify run (reuse hit): exit %d stderr=%s", code, stderr)
	}
	if gitInvocationsContainPush(t, logPath) {
		t.Fatal("expected no `git push` on a reuse-hit run")
	}
	if !strings.Contains(string(stdout), `"reused"`) {
		t.Fatalf("expected the second run to actually take the reuse path (reused commands in the receipt), got stdout=%s", stdout)
	}

	afterRefs := gitVerifyAt(t, remote, "for-each-ref")
	if beforeRefs == afterSetupPublishRefs {
		t.Fatal("test setup's own receipt publish did not change the remote - the safety comparison below would be meaningless")
	}
	if afterSetupPublishRefs != afterRefs {
		t.Fatalf("expected the origin remote's refs to be unchanged by made verify itself, after setup publish=%q after verify run=%q", afterSetupPublishRefs, afterRefs)
	}

	if _, err := os.Stat(filepath.Join(madeHomeDir, "gates")); !os.IsNotExist(err) {
		t.Fatalf("expected no gate directory to be created under MADE_HOME, err=%v", err)
	}
	if _, err := os.Stat(api.SocketPath(madeHomeDir)); !os.IsNotExist(err) {
		t.Fatalf("expected no daemon socket to be created under MADE_HOME, err=%v", err)
	}
}
