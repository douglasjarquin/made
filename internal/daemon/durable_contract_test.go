package daemon

import (
	"context"
	"testing"
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
	if err := reopened.Drain(submission); err != nil {
		t.Fatalf("Drain: %v", err)
	}
	if reopened.HasPending() {
		t.Fatal("drained submission remained pending")
	}
}
