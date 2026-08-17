package daemon

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

func TestPersistentRunStateIncludesSubmissionAndDecisionData(t *testing.T) {
	path := t.TempDir() + "/runs.wal"
	rm, err := NewPersistentRunManager(path)
	if err != nil {
		t.Fatalf("NewPersistentRunManager: %v", err)
	}
	id := "123e4567-e89b-12d3-a456-426614174002"
	if _, err := rm.SubmitWithMetadata(id, "repo", "feature", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", func(context.Context, func(Event)) error {
		return nil
	}); err != nil {
		t.Fatalf("SubmitWithMetadata: %v", err)
	}
	if err := rm.SetDecision(id, "review", ReviewApproved); err != nil {
		t.Fatalf("SetDecision: %v", err)
	}
	if err := rm.SetPRURL(id, "https://github.com/example/repo/pull/42"); err != nil {
		t.Fatalf("SetPRURL: %v", err)
	}
	if err := rm.AppendSubmissionEvent(id, SubmissionEvent{Gate: "gate", Ref: "refs/heads/feature", InputSHA: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Kind: "push"}); err != nil {
		t.Fatalf("AppendSubmissionEvent: %v", err)
	}
	if err := rm.Finish(id, RunAwaitingMerge, "PR is open"); err != nil {
		t.Fatalf("Finish: %v", err)
	}

	restarted, err := NewPersistentRunManager(path)
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	snapshot, ok := restarted.Snapshot(id)
	if !ok {
		t.Fatal("run missing after restart")
	}
	if snapshot.InputSHA != "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" || snapshot.OutputSHA != "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" {
		t.Fatalf("immutable heads lost after restart: %+v", snapshot)
	}
	if snapshot.Status != RunAwaitingMerge || snapshot.PRURL == "" || snapshot.Decisions["review"] != ReviewApproved || len(snapshot.SubmissionEvents) != 1 {
		t.Fatalf("durable state incomplete after restart: %+v", snapshot)
	}
}

func TestRunStoreRedactsDurableFindingAndErrorText(t *testing.T) {
	path := t.TempDir() + "/runs.wal"
	store, _, err := OpenRunStore(path)
	if err != nil {
		t.Fatalf("OpenRunStore: %v", err)
	}
	secret := "token=durable-secret"
	if err := store.Append(RunSnapshot{
		ID:       "123e4567-e89b-12d3-a456-426614174006",
		Message:  secret,
		Errors:   []string{secret},
		Findings: []RunFinding{{Message: secret}},
	}); err != nil {
		t.Fatalf("Append: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read WAL: %v", err)
	}
	if strings.Contains(string(data), "durable-secret") {
		t.Fatalf("WAL retained sensitive text: %s", data)
	}
}

func TestRunStoreIgnoresTornFinalRecord(t *testing.T) {
	path := t.TempDir() + "/runs.wal"
	store, _, err := OpenRunStore(path)
	if err != nil {
		t.Fatalf("OpenRunStore: %v", err)
	}
	seed := RunSnapshot{ID: "123e4567-e89b-12d3-a456-426614174007", Status: RunSucceeded}
	if err := store.Append(seed); err != nil {
		t.Fatalf("Append seed: %v", err)
	}
	appendBytes(t, path, []byte(`{"version":1,"kind":"snapshot"`))
	reopened, snapshots, err := OpenRunStore(path)
	if err != nil {
		t.Fatalf("OpenRunStore with torn tail: %v", err)
	}
	if reopened == nil || snapshots[seed.ID].Status != RunSucceeded {
		t.Fatalf("valid WAL record was lost after torn tail: %+v", snapshots)
	}
}

func TestGateSpoolIgnoresTornFinalRecord(t *testing.T) {
	path := t.TempDir() + "/gate.spool"
	spool, err := OpenGateSpool(path)
	if err != nil {
		t.Fatalf("OpenGateSpool: %v", err)
	}
	submission := GateSubmission{Gate: "gate", Ref: "refs/heads/main", SHA: "abc", RunID: "run"}
	if _, _, err := spool.Enqueue(submission); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	appendBytes(t, path, []byte(`{"kind":"enqueue","submission"`))
	reopened, err := OpenGateSpool(path)
	if err != nil {
		t.Fatalf("OpenGateSpool with torn tail: %v", err)
	}
	if pending := reopened.Pending(); len(pending) != 1 || pending[0] != submission {
		t.Fatalf("valid spool record was lost after torn tail: %+v", pending)
	}
}

func appendBytes(t *testing.T, path string, data []byte) {
	t.Helper()
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		t.Fatalf("open append fixture: %v", err)
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		t.Fatalf("write append fixture: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close append fixture: %v", err)
	}
}

func TestGateSpoolIsIdempotentAndDurable(t *testing.T) {
	path := t.TempDir() + "/gate.spool"
	spool, err := OpenGateSpool(path)
	if err != nil {
		t.Fatalf("OpenGateSpool: %v", err)
	}
	submission := GateSubmission{Gate: "gate", Ref: "refs/heads/main", SHA: "abc", RunID: "run"}
	if _, created, err := spool.Enqueue(submission); err != nil || !created {
		t.Fatalf("first Enqueue: created=%v err=%v", created, err)
	}
	if _, created, err := spool.Enqueue(submission); err != nil || created {
		t.Fatalf("duplicate Enqueue: created=%v err=%v", created, err)
	}
	reopened, err := OpenGateSpool(path)
	if err != nil {
		t.Fatalf("reopen spool: %v", err)
	}
	if !reopened.HasPending() {
		t.Fatal("pending submission disappeared after restart")
	}
	if pending := reopened.Pending(); len(pending) != 1 || pending[0] != submission {
		t.Fatalf("Pending returned %+v, want %+v", pending, submission)
	}
	if err := reopened.Drain(submission); err != nil {
		t.Fatalf("Drain: %v", err)
	}
	if reopened.HasPending() {
		t.Fatal("drained submission remained pending")
	}
}

func TestPersistentRunManagerReconcilesUnfinishedExecutionAfterRestart(t *testing.T) {
	path := t.TempDir() + "/runs.wal"
	store, _, err := OpenRunStore(path)
	if err != nil {
		t.Fatalf("OpenRunStore: %v", err)
	}
	id := "123e4567-e89b-12d3-a456-426614174003"
	if err := store.Append(RunSnapshot{
		ID:        id,
		Repo:      "repo",
		Branch:    "feature",
		Status:    RunRunning,
		QueuedAt:  time.Now().Add(-time.Minute),
		StartedAt: time.Now().Add(-30 * time.Second),
	}); err != nil {
		t.Fatalf("seed unfinished snapshot: %v", err)
	}
	reviewID := "123e4567-e89b-12d3-a456-426614174005"
	if err := store.Append(RunSnapshot{
		ID: reviewID, Repo: "repo", Branch: "review", Status: RunAwaitingReview,
		QueuedAt: time.Now().Add(-time.Minute), StartedAt: time.Now().Add(-30 * time.Second),
		PendingFindings: []AskUserFinding{{Stage: "review", Message: "approve"}},
	}); err != nil {
		t.Fatalf("seed awaiting-review snapshot: %v", err)
	}

	rm, err := NewPersistentRunManager(path)
	if err != nil {
		t.Fatalf("NewPersistentRunManager: %v", err)
	}
	snapshot, ok := rm.Snapshot(id)
	if !ok {
		t.Fatal("reconciled run missing")
	}
	if snapshot.Status != RunFailed || !snapshot.ExecutionFinished || snapshot.Err == nil {
		t.Fatalf("unfinished run was not reconciled to durable failure: %+v", snapshot)
	}
	reviewSnapshot, ok := rm.Snapshot(reviewID)
	if !ok || reviewSnapshot.Status != RunFailed || !reviewSnapshot.ExecutionFinished {
		t.Fatalf("awaiting-review run was not reconciled to durable failure: %+v", reviewSnapshot)
	}

	restarted, err := NewPersistentRunManager(path)
	if err != nil {
		t.Fatalf("reopen reconciled store: %v", err)
	}
	if snapshot, _ := restarted.Snapshot(id); snapshot.Status != RunFailed || !snapshot.ExecutionFinished {
		t.Fatalf("reconciled failure was not durable: %+v", snapshot)
	}
}
