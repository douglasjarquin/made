package gitgate_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/douglasjarquin/made/internal/gitgate"
)

// madeStubScript is InstallHooks' "made" binary for these tests: it logs its
// own argv (one line per invocation) to $MADE_STUB_LOG and always exits 0,
// so both pre-receive's admit-push and post-receive's notify-push let the
// push through without needing a real daemon.
const madeStubScript = "#!/bin/sh\necho \"$@\" >> \"$MADE_STUB_LOG\"\nexit 0\n"

func setupPushOptionGate(t *testing.T) (barePath, stubLogPath string) {
	t.Helper()
	dir := t.TempDir()
	barePath = filepath.Join(dir, "gate.git")
	if err := gitgate.InitBare(barePath); err != nil {
		t.Fatalf("InitBare: %v", err)
	}
	madeBinaryPath := filepath.Join(dir, "made-stub")
	if err := os.WriteFile(madeBinaryPath, []byte(madeStubScript), 0o755); err != nil {
		t.Fatalf("write made stub: %v", err)
	}
	madeHome := filepath.Join(dir, "made-home")
	if err := gitgate.InstallHooks(barePath, madeBinaryPath, madeHome); err != nil {
		t.Fatalf("InstallHooks: %v", err)
	}
	stubLogPath = filepath.Join(dir, "stub.log")
	return barePath, stubLogPath
}

func pushOptionGitEnv(stubLogPath string) []string {
	return append(os.Environ(),
		"MADE_STUB_LOG="+stubLogPath,
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_SYSTEM=/dev/null",
		"GIT_AUTHOR_NAME=hook-test",
		"GIT_AUTHOR_EMAIL=hook-test@example.com",
		"GIT_COMMITTER_NAME=hook-test",
		"GIT_COMMITTER_EMAIL=hook-test@example.com",
	)
}

func gitPushOptionCmd(t *testing.T, dir string, stubLogPath string, args ...string) *exec.Cmd {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = pushOptionGitEnv(stubLogPath)
	return cmd
}

func newPushOptionSourceRepo(t *testing.T, barePath, stubLogPath string) string {
	t.Helper()
	src := t.TempDir()
	for _, args := range [][]string{
		{"init", "-q"},
		{"commit", "-q", "--allow-empty", "-m", "initial"},
		{"remote", "add", "gate", barePath},
	} {
		if out, err := gitPushOptionCmd(t, src, stubLogPath, args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	return src
}

func TestPushOption_RoundTripsThroughHookToNotifyPush(t *testing.T) {
	barePath, stubLogPath := setupPushOptionGate(t)
	src := newPushOptionSourceRepo(t, barePath, stubLogPath)

	out, err := gitPushOptionCmd(t, src, stubLogPath, "push", "-o", "agent=claude", "gate", "HEAD:refs/heads/main").CombinedOutput()
	if err != nil {
		t.Fatalf("git push -o agent=claude: %v: %s", err, out)
	}

	log, err := os.ReadFile(stubLogPath)
	if err != nil {
		t.Fatalf("read stub log: %v", err)
	}
	if !strings.Contains(string(log), "gate notify-push") || !strings.Contains(string(log), "--agent-preference claude") {
		t.Fatalf("expected notify-push invocation with --agent-preference claude, stub log:\n%s", log)
	}
}

func TestPushOption_AbsentWhenNoPushOptionGiven(t *testing.T) {
	barePath, stubLogPath := setupPushOptionGate(t)
	src := newPushOptionSourceRepo(t, barePath, stubLogPath)

	out, err := gitPushOptionCmd(t, src, stubLogPath, "push", "gate", "HEAD:refs/heads/main").CombinedOutput()
	if err != nil {
		t.Fatalf("git push: %v: %s", err, out)
	}

	log, err := os.ReadFile(stubLogPath)
	if err != nil {
		t.Fatalf("read stub log: %v", err)
	}
	if strings.Contains(string(log), "--agent-preference") {
		t.Errorf("expected no --agent-preference in any invocation when -o was never used, stub log:\n%s", log)
	}
}

func TestPushOption_ShellMetacharacterValueNeverExecuted(t *testing.T) {
	barePath, stubLogPath := setupPushOptionGate(t)
	src := newPushOptionSourceRepo(t, barePath, stubLogPath)
	canary := filepath.Join(t.TempDir(), "pwned")

	out, err := gitPushOptionCmd(t, src, stubLogPath, "push", "-o", "agent=claude$(touch "+canary+")", "gate", "HEAD:refs/heads/main").CombinedOutput()
	if err != nil {
		t.Fatalf("git push -o with shell metacharacters: %v: %s", err, out)
	}

	if _, statErr := os.Stat(canary); statErr == nil {
		t.Fatalf("shell metacharacter push-option value was executed - canary file %s was created", canary)
	}
}

// buildRealMadeBinary compiles the actual cmd/made binary once per test
// process: TestPushOption_SelfHealsAnOldGateMissingAdvertisePushOptions
// needs the real self-heal code path (runGateNotifyPushCommand calling
// gitgate.EnableAdvertisePushOptions), not the fake argv-logging stub every
// other test in this file uses.
func buildRealMadeBinary(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "made")
	cmd := exec.Command("go", "build", "-o", bin, "github.com/douglasjarquin/made/cmd/made")
	cmd.Dir = repoRootForBuild(t)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build real made binary: %v: %s", err, out)
	}
	return bin
}

// repoRootForBuild walks up from the current package directory to the
// module root, so `go build` resolves github.com/douglasjarquin/made/cmd/made
// regardless of the test binary's own working directory.
func repoRootForBuild(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("could not find go.mod above %s", dir)
		}
		dir = parent
	}
}

func TestPushOption_SelfHealsAnOldGateMissingAdvertisePushOptions(t *testing.T) {
	dir := t.TempDir()
	barePath := filepath.Join(dir, "gate.git")
	// Simulate a pre-agent-auto-resolve gate: git init --bare directly,
	// never calling gitgate.InitBare, so advertisePushOptions is unset.
	if out, err := exec.Command("git", "init", "--bare", barePath).CombinedOutput(); err != nil {
		t.Fatalf("git init --bare: %v: %s", err, out)
	}
	madeBinaryPath := buildRealMadeBinary(t)
	if err := gitgate.InstallHooks(barePath, madeBinaryPath, filepath.Join(dir, "made-home")); err != nil {
		t.Fatalf("InstallHooks: %v", err)
	}
	// admit-push (pre-receive) needs a live daemon dial with the real
	// binary, which is orthogonal to what this test verifies (notify-push's
	// self-heal) - neuter it to an unconditional pass so the push reaches
	// post-receive without needing a real daemon running.
	if err := os.WriteFile(filepath.Join(barePath, "hooks", "pre-receive"), []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatalf("overwrite pre-receive: %v", err)
	}
	stubLogPath := filepath.Join(dir, "stub.log")
	src := newPushOptionSourceRepo(t, barePath, stubLogPath)

	// First push with -o against the un-healed gate is expected to fail
	// (git rejects push options the server never advertised) - this
	// documents the real, accepted limitation (D5): self-heal only
	// benefits the NEXT push, not this one, since capability negotiation
	// happens before either hook can run.
	if out, err := gitPushOptionCmd(t, src, stubLogPath, "push", "-o", "agent=claude", "gate", "HEAD:refs/heads/main").CombinedOutput(); err == nil {
		t.Fatalf("expected the first -o push against an unhealed gate to fail, got:\n%s", out)
	}

	// A plain push (no -o) still succeeds and, per D5, self-heals the gate.
	if out, err := gitPushOptionCmd(t, src, stubLogPath, "push", "gate", "HEAD:refs/heads/main").CombinedOutput(); err != nil {
		t.Fatalf("git push (healing): %v: %s", err, out)
	}

	// The next -o push now succeeds against the self-healed gate.
	if out, err := gitPushOptionCmd(t, src, stubLogPath, "push", "-o", "agent=claude", "gate", "HEAD:refs/heads/main").CombinedOutput(); err != nil {
		t.Fatalf("git push -o agent=claude after self-heal: %v: %s", err, out)
	}
}
