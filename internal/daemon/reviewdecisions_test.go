package daemon

import (
	"context"
	"testing"
	"time"
)

func TestReviewDecisions_WaitUnblocksOnSet(t *testing.T) {
	d := NewReviewDecisions()

	type outcome struct {
		decision string
		err      error
	}
	resultCh := make(chan outcome, 1)

	go func() {
		decision, err := d.Wait(context.Background(), "run-1", "review")
		resultCh <- outcome{decision, err}
	}()

	waitForWaiterRegistered(t, d, "run-1", "review")

	if err := d.Set("run-1", "review", ReviewApproved); err != nil {
		t.Fatalf("Set: %v", err)
	}

	select {
	case got := <-resultCh:
		if got.err != nil {
			t.Fatalf("Wait returned error: %v", got.err)
		}
		if got.decision != ReviewApproved {
			t.Fatalf("decision = %q, want %q", got.decision, ReviewApproved)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Wait did not unblock within timeout")
	}
}

func TestReviewDecisions_WaitReturnsImmediatelyIfAlreadyRecorded(t *testing.T) {
	d := NewReviewDecisions()
	if err := d.Set("run-2", "document", ReviewRejected); err != nil {
		t.Fatalf("Set: %v", err)
	}

	resultCh := make(chan string, 1)
	errCh := make(chan error, 1)
	go func() {
		decision, err := d.Wait(context.Background(), "run-2", "document")
		if err != nil {
			errCh <- err
			return
		}
		resultCh <- decision
	}()

	select {
	case decision := <-resultCh:
		if decision != ReviewRejected {
			t.Fatalf("decision = %q, want %q", decision, ReviewRejected)
		}
	case err := <-errCh:
		t.Fatalf("Wait returned error: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("Wait did not return promptly for an already-recorded decision")
	}
}

func TestReviewDecisions_WaitRespectsContextCancellation(t *testing.T) {
	d := NewReviewDecisions()

	ctx, cancel := context.WithCancel(context.Background())
	resultCh := make(chan error, 1)
	go func() {
		_, err := d.Wait(ctx, "run-3", "review")
		resultCh <- err
	}()

	waitForWaiterRegistered(t, d, "run-3", "review")
	cancel()

	select {
	case err := <-resultCh:
		if err == nil {
			t.Fatal("Wait returned nil error after context cancellation")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Wait did not return promptly after context cancellation")
	}

	d.mu.Lock()
	remaining := len(d.waiters[reviewKey{RunID: "run-3", Stage: "review"}])
	d.mu.Unlock()
	if remaining != 0 {
		t.Fatalf("waiter not cleaned up after cancellation: %d remaining", remaining)
	}
}

func waitForWaiterRegistered(t *testing.T, d *ReviewDecisions, runID, stage string) {
	t.Helper()
	key := reviewKey{RunID: runID, Stage: stage}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		d.mu.Lock()
		n := len(d.waiters[key])
		d.mu.Unlock()
		if n > 0 {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("waiter never registered")
}
