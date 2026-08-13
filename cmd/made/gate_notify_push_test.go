package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/douglasjarquin/made/internal/daemon"
)

const gitZeroSHA = "0000000000000000000000000000000000000000"

// setupGateFixture builds a real gate via the same gateInit entry point
// `made gate init` uses, then removes its installed hooks: these tests
// drive gate.notifyPush directly (as the post-receive hook would), so a
// fixture push through the "made" remote must not also trigger the real
// pre-receive/post-receive scripts (which shell a real made binary path
// these tests don't have).
func setupGateFixture(t *testing.T, home string) (barePath, sourceDir string) {
	t.Helper()
	scratch := shortTempDir(t)
	remoteDir := filepath.Join(scratch, "remote.git")
	sourceDir = filepath.Join(scratch, "source")
	remoteURL := "file://" + remoteDir

	testGit(t, "", "init", "--bare", "-b", "main", remoteDir)
	testGit(t, "", "init", "-b", "main", sourceDir)
	writeAndCommit(t, sourceDir, "README.md", "hello\n", "init")
	testGit(t, sourceDir, "remote", "add", "origin", remoteURL)
	testGit(t, sourceDir, "push", "origin", "main")

	barePath, err := gateInit(context.Background(), home, "/usr/local/bin/made", sourceDir, remoteURL)
	if err != nil {
		t.Fatalf("gateInit: %v", err)
	}
	for _, hook := range []string{"pre-receive", "post-receive"} {
		if err := os.Remove(filepath.Join(barePath, "hooks", hook)); err != nil {
			t.Fatalf("remove hook %s: %v", hook, err)
		}
	}
	return barePath, sourceDir
}

func pushFeatureCommit(t *testing.T, sourceDir, branch, fileContent, message string) string {
	t.Helper()
	writeAndCommit(t, sourceDir, "feature.txt", fileContent, message)
	sha := strings.TrimSpace(testGitOutput(t, sourceDir, "rev-parse", "HEAD"))
	testGit(t, sourceDir, "push", "made", branch)
	return sha
}

func waitForRunTerminal(t *testing.T, rm *daemon.RunManager, id string, timeout time.Duration) daemon.RunSnapshot {
	t.Helper()
	deadline := time.After(timeout)
	for {
		snap, ok := rm.Snapshot(id)
		if ok && (snap.Status == daemon.RunCompleted || snap.Status == daemon.RunFailed) {
			return snap
		}
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for run %q to reach a terminal state (last seen %+v, ok=%v)", id, snap, ok)
		case <-time.After(5 * time.Millisecond):
		}
	}
}

func TestGateNotifyPushRPC_NormalFeatureBranchPushCreatesRun(t *testing.T) {
	home := shortTempDir(t)
	rm, client := startTestDaemon(t, home)
	barePath, sourceDir := setupGateFixture(t, home)

	testGit(t, sourceDir, "checkout", "-b", "feature-x")
	sha := pushFeatureCommit(t, sourceDir, "feature-x", "v1\n", "feature commit 1")

	var result gateNotifyPushResult
	if err := client.CallInto("gate.notifyPush", gateNotifyPushParams{
		GatePath: barePath,
		OldSHA:   gitZeroSHA,
		NewSHA:   sha,
		Ref:      "refs/heads/feature-x",
	}, &result); err != nil {
		t.Fatalf("gate.notifyPush: %v", err)
	}
	if result.RunID == "" {
		t.Fatal("expected a run ID for a normal feature branch push")
	}

	snap := waitForRunTerminal(t, rm, result.RunID, 10*time.Second)
	if snap.Status != daemon.RunCompleted {
		t.Fatalf("expected run to complete, got status=%v err=%v", snap.Status, snap.Err)
	}
	if snap.Branch != "feature-x" {
		t.Fatalf("expected run tracked for branch feature-x, got %+v", snap)
	}
}

func TestGateNotifyPushRPC_RejectedRefCreatesNoRun(t *testing.T) {
	home := shortTempDir(t)
	rm, client := startTestDaemon(t, home)
	barePath, _ := setupGateFixture(t, home)

	cases := []struct {
		name string
		ref  string
	}{
		{"default branch", "refs/heads/main"},
		{"tag ref", "refs/tags/v1"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			before := len(rm.List())
			_, err := client.Call("gate.notifyPush", gateNotifyPushParams{
				GatePath: barePath,
				OldSHA:   gitZeroSHA,
				NewSHA:   strings.Repeat("a", 40),
				Ref:      tc.ref,
			})
			if err == nil {
				t.Fatalf("expected gate.notifyPush to reject ref %s", tc.ref)
			}
			if after := len(rm.List()); after != before {
				t.Fatalf("expected no new run for rejected ref %s, run count %d -> %d", tc.ref, before, after)
			}
		})
	}
}

func TestGateNotifyPushRPC_RefDeletionCreatesNoRun(t *testing.T) {
	home := shortTempDir(t)
	rm, client := startTestDaemon(t, home)
	barePath, _ := setupGateFixture(t, home)

	before := len(rm.List())
	var result gateNotifyPushResult
	if err := client.CallInto("gate.notifyPush", gateNotifyPushParams{
		GatePath: barePath,
		OldSHA:   strings.Repeat("a", 40),
		NewSHA:   gitZeroSHA,
		Ref:      "refs/heads/feature-deleted",
	}, &result); err != nil {
		t.Fatalf("gate.notifyPush (deletion): %v", err)
	}
	if result.RunID != "" {
		t.Fatalf("expected no run ID for a ref deletion, got %q", result.RunID)
	}
	if after := len(rm.List()); after != before {
		t.Fatalf("expected no new run for a ref deletion, run count %d -> %d", before, after)
	}
}

func TestGateNotifyPushRPC_SupersededPushValidatesNewestSHA(t *testing.T) {
	home := shortTempDir(t)
	rm, client := startTestDaemon(t, home)
	barePath, sourceDir := setupGateFixture(t, home)

	testGit(t, sourceDir, "checkout", "-b", "feature-x")
	sha1 := pushFeatureCommit(t, sourceDir, "feature-x", "v1\n", "feature commit 1")
	sha2 := pushFeatureCommit(t, sourceDir, "feature-x", "v2\n", "feature commit 2")
	if sha1 == sha2 {
		t.Fatal("test setup bug: expected two distinct commits")
	}

	repo := gateRepoIdentifier(barePath)

	blockStarted := make(chan struct{})
	blockRelease := make(chan struct{})
	blockID := rm.NewRunID()
	if _, err := rm.Submit(blockID, repo, "unrelated-branch", func(ctx context.Context, emit func(daemon.Event)) error {
		close(blockStarted)
		<-blockRelease
		return nil
	}); err != nil {
		t.Fatalf("submit blocker: %v", err)
	}
	<-blockStarted

	var result1 gateNotifyPushResult
	if err := client.CallInto("gate.notifyPush", gateNotifyPushParams{
		GatePath: barePath,
		OldSHA:   gitZeroSHA,
		NewSHA:   sha1,
		Ref:      "refs/heads/feature-x",
	}, &result1); err != nil {
		t.Fatalf("gate.notifyPush (first): %v", err)
	}
	if result1.RunID == "" {
		t.Fatal("expected a run ID for the first push")
	}

	if snap, ok := rm.Snapshot(result1.RunID); !ok || snap.Status != daemon.RunQueued {
		t.Fatalf("expected first run still queued behind the blocker, got %+v (ok=%v)", snap, ok)
	}

	var result2 gateNotifyPushResult
	if err := client.CallInto("gate.notifyPush", gateNotifyPushParams{
		GatePath: barePath,
		OldSHA:   sha1,
		NewSHA:   sha2,
		Ref:      "refs/heads/feature-x",
	}, &result2); err != nil {
		t.Fatalf("gate.notifyPush (second): %v", err)
	}
	if result2.RunID == "" {
		t.Fatal("expected a run ID for the second push")
	}

	events, unsubscribe := rm.Subscribe(result2.RunID)
	defer unsubscribe()

	close(blockRelease)

	var sawSetupMessage string
	deadline := time.After(10 * time.Second)
waitLoop:
	for {
		select {
		case ev := <-events:
			if ev.Kind == daemon.EventStageStarted && ev.Stage == "setup" {
				sawSetupMessage = ev.Message
			}
			if ev.Kind == daemon.EventRunCompleted || ev.Kind == daemon.EventRunFailed {
				break waitLoop
			}
		case <-deadline:
			t.Fatal("timed out waiting for the second (superseding) run to complete")
		}
	}

	if !strings.Contains(sawSetupMessage, sha2) {
		t.Fatalf("expected the surviving run's setup stage to reference the newest SHA %s, got message %q", sha2, sawSetupMessage)
	}
	if strings.Contains(sawSetupMessage, sha1) {
		t.Fatalf("did not expect the surviving run's setup stage to reference the superseded SHA %s, got message %q", sha1, sawSetupMessage)
	}

	final1, ok := rm.Snapshot(result1.RunID)
	if !ok {
		t.Fatal("expected the first (superseded) run to remain tracked")
	}
	if final1.Status != daemon.RunFailed || !errors.Is(final1.Err, daemon.ErrRunSuperseded) {
		t.Fatalf("expected first run superseded (Failed/ErrRunSuperseded), got status=%v err=%v", final1.Status, final1.Err)
	}
	if !final1.StartedAt.IsZero() {
		t.Fatal("superseded run must never have started")
	}

	final2 := waitForRunTerminal(t, rm, result2.RunID, 2*time.Second)
	if final2.Status != daemon.RunCompleted {
		t.Fatalf("expected the second (surviving) run to complete, got status=%v err=%v", final2.Status, final2.Err)
	}
}

func TestGateNotifyPushCLI_AlwaysExitsZeroEvenWhenDaemonUnreachable(t *testing.T) {
	home := shortTempDir(t)
	t.Setenv("MADE_HOME", home)

	done := make(chan struct{})
	var out, errOut []byte
	var code int
	go func() {
		out, errOut, code = runCapture(t, []string{
			"gate", "notify-push",
			"--gate", filepath.Join(home, "gates", "does-not-matter", "gate.git"),
			"--old", gitZeroSHA,
			"--new", strings.Repeat("b", 40),
			"--ref", "refs/heads/feature-x",
		})
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("made gate notify-push hung with no daemon running")
	}

	if code != 0 {
		t.Fatalf("expected exit code 0 even when the daemon is unreachable, got %d; stdout=%s stderr=%s", code, out, errOut)
	}
	if strings.TrimSpace(string(errOut)) == "" {
		t.Fatal("expected a diagnostic message on stderr even though the exit code must stay 0")
	}
}

func TestGateNotifyPushCLI_NormalPushExitsZero(t *testing.T) {
	home := shortTempDir(t)
	t.Setenv("MADE_HOME", home)
	_, _ = startTestDaemon(t, home)

	barePath, sourceDir := setupGateFixture(t, home)
	testGit(t, sourceDir, "checkout", "-b", "feature-x")
	sha := pushFeatureCommit(t, sourceDir, "feature-x", "v1\n", "feature commit 1")

	out, errOut, code := runCapture(t, []string{
		"gate", "notify-push",
		"--gate", barePath,
		"--old", gitZeroSHA,
		"--new", sha,
		"--ref", "refs/heads/feature-x",
	})
	if code != 0 {
		t.Fatalf("gate notify-push exit code = %d, want 0; stdout=%s stderr=%s", code, out, errOut)
	}
}
