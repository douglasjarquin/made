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

// pushFeatureCommit's commit message always carries an "Intent: <message>"
// trailer so the real orchestrator's intent stage (Task 12) passes and its
// per-stage event carries the message back out, giving tests a real signal
// to distinguish which commit a run actually validated.
func pushFeatureCommit(t *testing.T, sourceDir, branch, fileContent, message string) string {
	t.Helper()
	writeAndCommit(t, sourceDir, "feature.txt", fileContent, message+"\n\nIntent: "+message)
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

	// This fixture has no configured agent/test/lint commands, so the real
	// orchestrator (Task 12) is expected to run for real and fail early
	// (at the review stage's agent resolution) rather than complete - the
	// point of this test is that a normal push actually gets orchestrated
	// at all, not that this bare fixture passes every stage.
	snap := waitForRunTerminal(t, rm, result.RunID, 10*time.Second)
	if snap.Branch != "feature-x" {
		t.Fatalf("expected run tracked for branch feature-x, got %+v", snap)
	}
	if snap.StartedAt.IsZero() || len(snap.Stages) == 0 {
		t.Fatalf("expected the real orchestrated pipeline to have actually started and recorded stages, got %+v", snap)
	}
	if snap.Stages[0].Name != "intent" || snap.Stages[0].Result != "pass" {
		t.Fatalf("expected the intent stage to pass for a commit with a real Intent trailer, got %+v", snap.Stages)
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

func TestGateNotifyPushRPC_RejectsNewSHAThatIsNotTheReceivedRef(t *testing.T) {
	home := shortTempDir(t)
	rm, client := startTestDaemon(t, home)
	barePath, sourceDir := setupGateFixture(t, home)

	testGit(t, sourceDir, "checkout", "-b", "feature-forged")
	_ = pushFeatureCommit(t, sourceDir, "feature-forged", "v1\n", "feature commit")
	_, err := client.Call("gate.notifyPush", gateNotifyPushParams{
		GatePath: barePath,
		OldSHA:   gitZeroSHA,
		NewSHA:   strings.Repeat("a", 40),
		Ref:      "refs/heads/feature-forged",
	})
	if err == nil {
		t.Fatalf("accepted forged new SHA; runs=%+v", rm.List())
	}
}

func TestGateNotifyPushRPC_RejectsExistingUnrelatedSHA(t *testing.T) {
	home := shortTempDir(t)
	rm, client := startTestDaemon(t, home)
	barePath, sourceDir := setupGateFixture(t, home)

	testGit(t, sourceDir, "checkout", "-b", "feature-forged")
	featureSHA := pushFeatureCommit(t, sourceDir, "feature-forged", "v1\n", "feature commit")

	unrelatedDir := shortTempDir(t)
	testGit(t, "", "init", "-b", "unrelated-history", unrelatedDir)
	writeAndCommit(t, unrelatedDir, "unrelated.txt", "unrelated\n", "unrelated commit")
	unrelatedSHA := strings.TrimSpace(testGitOutput(t, unrelatedDir, "rev-parse", "HEAD"))
	testGit(t, unrelatedDir, "remote", "add", "origin", "file://"+barePath)
	testGit(t, unrelatedDir, "push", "origin", "HEAD:refs/heads/unrelated-history")

	_, err := client.Call("gate.notifyPush", gateNotifyPushParams{
		GatePath: barePath,
		OldSHA:   gitZeroSHA,
		NewSHA:   unrelatedSHA,
		Ref:      "refs/heads/feature-forged",
	})
	if err == nil {
		t.Fatalf("accepted existing unrelated SHA %s for feature SHA %s; runs=%+v", unrelatedSHA, featureSHA, rm.List())
	}
}

func TestGateNotifyPushRPC_RejectsStaleAncestorSHA(t *testing.T) {
	home := shortTempDir(t)
	rm, client := startTestDaemon(t, home)
	barePath, sourceDir := setupGateFixture(t, home)

	testGit(t, sourceDir, "checkout", "-b", "feature-stale")
	sha1 := pushFeatureCommit(t, sourceDir, "feature-stale", "v1\n", "feature commit 1")
	_ = pushFeatureCommit(t, sourceDir, "feature-stale", "v2\n", "feature commit 2")

	_, err := client.Call("gate.notifyPush", gateNotifyPushParams{
		GatePath: barePath,
		OldSHA:   gitZeroSHA,
		NewSHA:   sha1,
		Ref:      "refs/heads/feature-stale",
	})
	if err == nil {
		t.Fatalf("accepted stale ancestor SHA %s for advanced feature ref; runs=%+v", sha1, rm.List())
	}
	if runs := rm.List(); len(runs) != 0 {
		t.Fatalf("stale notification created runs: %+v", runs)
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

	sha2 := pushFeatureCommit(t, sourceDir, "feature-x", "v2\n", "feature commit 2")
	if sha1 == sha2 {
		t.Fatal("test setup bug: expected two distinct commits")
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

	// The real orchestrator's intent stage (Task 12) reads the worktree's own
	// commit trailer and reports it back via its EventStageFinished message,
	// giving a real signal (in place of the old placeholder's synthetic
	// "setup" event) for which commit - sha1 or sha2 - a run actually
	// validated.
	var sawIntentMessage string
	deadline := time.After(10 * time.Second)
waitLoop:
	for {
		select {
		case ev := <-events:
			if ev.Kind == daemon.EventStageFinished && ev.Stage == "intent" {
				sawIntentMessage = ev.Message
			}
			if ev.Kind == daemon.EventRunCompleted || ev.Kind == daemon.EventRunFailed {
				break waitLoop
			}
		case <-deadline:
			t.Fatal("timed out waiting for the second (superseding) run to reach a terminal state")
		}
	}

	if !strings.Contains(sawIntentMessage, "feature commit 2") {
		t.Fatalf("expected the surviving run's intent stage to validate the newest push (%q), got message %q", "feature commit 2", sawIntentMessage)
	}
	if strings.Contains(sawIntentMessage, "feature commit 1") {
		t.Fatalf("did not expect the surviving run's intent stage to reference the superseded push, got message %q", sawIntentMessage)
	}

	final1, ok := rm.Snapshot(result1.RunID)
	if !ok {
		t.Fatal("expected the first (superseded) run to remain tracked")
	}
	if final1.Status != daemon.RunSuperseded || !errors.Is(final1.Err, daemon.ErrRunSuperseded) {
		t.Fatalf("expected first run superseded (Superseded/ErrRunSuperseded), got status=%v err=%v", final1.Status, final1.Err)
	}
	if !final1.StartedAt.IsZero() {
		t.Fatal("superseded run must never have started")
	}

	// As with the normal-push test above, this bare fixture has no
	// configured agent, so the surviving run is expected to fail past intent
	// (at review's agent resolution) rather than complete - the point here is
	// exclusively which SHA it validated, asserted above.
	final2 := waitForRunTerminal(t, rm, result2.RunID, 2*time.Second)
	if final2.StartedAt.IsZero() {
		t.Fatalf("expected the second (surviving) run to have actually started, got status=%v err=%v", final2.Status, final2.Err)
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
