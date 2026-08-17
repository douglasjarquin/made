package daemon

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

const (
	ReviewApproved = "approved"
	ReviewRejected = "rejected"
)

var ErrDecisionAlreadyRecorded = errors.New("daemon: review decision already recorded")

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
	persist func(runID, stage, decision string) error
	manager *RunManager
}

func NewReviewDecisions() *ReviewDecisions {
	return &ReviewDecisions{
		entries: make(map[reviewKey]string),
		waiters: make(map[reviewKey][]chan string),
	}
}

func NewReviewDecisionsForManager(rm *RunManager) *ReviewDecisions {
	d := NewReviewDecisions()
	d.persist = rm.UpdateDecision
	d.manager = rm
	return d
}

// Set records a decision for (runID, stage) and wakes any goroutine blocked
// in Wait on that exact key.
func (d *ReviewDecisions) Set(runID, stage, decision string) error {
	key := reviewKey{RunID: runID, Stage: stage}
	if _, exists := d.Get(runID, stage); exists {
		return fmt.Errorf("%w for %s/%s", ErrDecisionAlreadyRecorded, runID, stage)
	}

	d.mu.Lock()
	if _, exists := d.entries[key]; exists {
		d.mu.Unlock()
		return fmt.Errorf("%w for %s/%s", ErrDecisionAlreadyRecorded, runID, stage)
	}
	d.entries[key] = decision
	waiters := d.waiters[key]
	if d.persist != nil {
		if err := d.persist(runID, stage, decision); err != nil {
			delete(d.entries, key)
			d.mu.Unlock()
			return fmt.Errorf("daemon: persist review decision: %w", err)
		}
	}
	delete(d.waiters, key)
	d.mu.Unlock()

	for _, ch := range waiters {
		ch <- decision
	}
	return nil
}

func (d *ReviewDecisions) Get(runID, stage string) (string, bool) {
	d.mu.Lock()
	decision, ok := d.entries[reviewKey{RunID: runID, Stage: stage}]
	d.mu.Unlock()
	if ok || d.persist == nil {
		return decision, ok
	}
	if d.manager != nil {
		if snapshot, found := d.manager.Snapshot(runID); found {
			decision, ok = snapshot.Decisions[stage]
			if ok {
				d.mu.Lock()
				d.entries[reviewKey{RunID: runID, Stage: stage}] = decision
				d.mu.Unlock()
			}
		}
	}
	return decision, ok
}

// Wait blocks until a decision is recorded for (runID, stage) via Set, or
// until ctx is done, whichever comes first. It returns immediately if a
// decision is already recorded.
func (d *ReviewDecisions) Wait(ctx context.Context, runID, stage string) (string, error) {
	key := reviewKey{RunID: runID, Stage: stage}
	if decision, ok := d.Get(runID, stage); ok {
		return decision, nil
	}

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
