package push_test

import (
	"context"
	"strings"
	"testing"

	"github.com/douglasjarquin/made/internal/pipeline/push"
)

func TestRun_SuccessfulPushUpdatesRemote(t *testing.T) {
	f := setupFixture(t)
	wt := f.addWorktree(t)
	defer func() {
		if err := wt.Remove(); err != nil {
			t.Errorf("Remove: %v", err)
		}
	}()

	writeFile(t, wt.Path, "validated.txt", "validated content\n")
	run(t, wt.Path, "add", ".")
	run(t, wt.Path, "commit", "-q", "-m", "validated commit")
	addRemote(t, wt.Path, "origin", f.remotePath)

	beforeSHA := strings.TrimSpace(runAllowFail(t, f.remotePath, "rev-parse", "--verify", "--quiet", "refs/heads/"+f.branch))
	if beforeSHA != "" {
		t.Fatalf("expected remote to have no %s ref yet, got %s", f.branch, beforeSHA)
	}

	result, err := push.Run(context.Background(), wt.Path, "origin", f.branch)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !result.OK {
		t.Fatalf("expected OK=true, got %+v", result)
	}

	localSHA := strings.TrimSpace(run(t, wt.Path, "rev-parse", "HEAD"))
	remoteRefs := run(t, f.remotePath, "show-ref", "--verify", "refs/heads/"+f.branch)
	if !strings.Contains(remoteRefs, localSHA) {
		t.Fatalf("expected remote ref refs/heads/%s to point at %s, got: %s", f.branch, localSHA, remoteRefs)
	}
}

func TestRun_UnreachableRemoteFailsClean(t *testing.T) {
	f := setupFixture(t)
	wt := f.addWorktree(t)
	defer func() {
		if err := wt.Remove(); err != nil {
			t.Errorf("Remove: %v", err)
		}
	}()

	writeFile(t, wt.Path, "validated.txt", "validated content\n")
	run(t, wt.Path, "add", ".")
	run(t, wt.Path, "commit", "-q", "-m", "validated commit")

	nonexistent := f.remotePath + "-does-not-exist"
	addRemote(t, wt.Path, "origin", nonexistent)

	beforeHead := strings.TrimSpace(run(t, wt.Path, "rev-parse", "HEAD"))

	result, err := push.Run(context.Background(), wt.Path, "origin", f.branch)
	if err != nil {
		t.Fatalf("Run: expected a reported failure, not an error, got: %v", err)
	}
	if result.OK {
		t.Fatalf("expected OK=false for a push to an unreachable remote, got %+v", result)
	}
	if result.Message == "" {
		t.Fatalf("expected a non-empty failure message")
	}

	status := run(t, wt.Path, "status", "--porcelain")
	if strings.TrimSpace(status) != "" {
		t.Fatalf("expected worktree to remain clean after failed push, got status:\n%s", status)
	}
	afterHead := strings.TrimSpace(run(t, wt.Path, "rev-parse", "HEAD"))
	if beforeHead != afterHead {
		t.Fatalf("expected HEAD to be unchanged after failed push, before=%s after=%s", beforeHead, afterHead)
	}

	log := run(t, wt.Path, "log", "-1", "--format=%s")
	if !strings.Contains(log, "validated commit") {
		t.Fatalf("expected worktree to still be usable (git log works), got: %s", log)
	}
}

func TestRun_DoesNotLeakCredentialsOnFailure(t *testing.T) {
	f := setupFixture(t)
	wt := f.addWorktree(t)
	defer func() {
		if err := wt.Remove(); err != nil {
			t.Errorf("Remove: %v", err)
		}
	}()

	writeFile(t, wt.Path, "validated.txt", "validated content\n")
	run(t, wt.Path, "add", ".")
	run(t, wt.Path, "commit", "-q", "-m", "validated commit")

	const secret = "supersecrettoken123"
	addRemote(t, wt.Path, "origin", "https://ci-bot:"+secret+"@127.0.0.1:1/repo.git")

	result, err := push.Run(context.Background(), wt.Path, "origin", f.branch)
	if err != nil && strings.Contains(err.Error(), secret) {
		t.Fatalf("credential leaked into returned error: %v", err)
	}
	if strings.Contains(result.Message, secret) {
		t.Fatalf("credential leaked into result message: %q", result.Message)
	}
}
