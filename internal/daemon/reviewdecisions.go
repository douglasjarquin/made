package daemon

import (
	"context"
	"sync"
)

const (
	ReviewApproved = "approved"
	ReviewRejected = "rejected"
)

type reviewKey struct {
	RunID string
	Stage string
}

// ReviewDecisions lives alongside RunManager because a decision only ever
// applies to one (run, stage) pair, making it per-run state that both the
// review.decide/review.decision RPC handlers and the orchestrator's WorkFunc
// need to reach from their separate packages.
type ReviewDecisions struct {
	mu      sync.Mutex
	entries map[reviewKey]string
	waiters map[reviewKey][]chan string
}

func NewReviewDecisions() *ReviewDecisions {
	return &ReviewDecisions{
		entries: make(map[reviewKey]string),
		waiters: make(map[reviewKey][]chan string),
	}
}

// Set records a decision for (runID, stage) and wakes any goroutine blocked
// in Wait on that exact key.
func (d *ReviewDecisions) Set(runID, stage, decision string) {
	key := reviewKey{RunID: runID, Stage: stage}

	d.mu.Lock()
	d.entries[key] = decision
	waiters := d.waiters[key]
	delete(d.waiters, key)
	d.mu.Unlock()

	for _, ch := range waiters {
		ch <- decision
	}
}

func (d *ReviewDecisions) Get(runID, stage string) (string, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	decision, ok := d.entries[reviewKey{RunID: runID, Stage: stage}]
	return decision, ok
}

// Wait blocks until a decision is recorded for (runID, stage) via Set, or
// until ctx is done, whichever comes first. It returns immediately if a
// decision is already recorded.
func (d *ReviewDecisions) Wait(ctx context.Context, runID, stage string) (string, error) {
	key := reviewKey{RunID: runID, Stage: stage}

	d.mu.Lock()
	if decision, ok := d.entries[key]; ok {
		d.mu.Unlock()
		return decision, nil
	}
	ch := make(chan string, 1)
	d.waiters[key] = append(d.waiters[key], ch)
	d.mu.Unlock()

	select {
	case decision := <-ch:
		return decision, nil
	case <-ctx.Done():
		d.removeWaiter(key, ch)
		return "", ctx.Err()
	}
}

func (d *ReviewDecisions) removeWaiter(key reviewKey, ch chan string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	list := d.waiters[key]
	for i, c := range list {
		if c == ch {
			d.waiters[key] = append(list[:i], list[i+1:]...)
			break
		}
	}
	if len(d.waiters[key]) == 0 {
		delete(d.waiters, key)
	}
}
