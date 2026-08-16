package daemon

import (
	"fmt"
	"time"
)

func (rm *RunManager) HasActive() bool {
	for _, snapshot := range rm.List() {
		switch snapshot.Status {
		case RunQueued, RunRunning, RunAwaitingReview, RunAwaitingMerge:
			return true
		}
	}
	return false
}

func (rm *RunManager) SetDecision(id, stage, decision string) error {
	r, ok := rm.lookupRun(id)
	if !ok {
		return fmt.Errorf("daemon: no run %q", id)
	}
	if stage == "" || decision == "" {
		return fmt.Errorf("daemon: decision stage and value are required")
	}
	r.update(func(snapshot *RunSnapshot) {
		if snapshot.Decisions == nil {
			snapshot.Decisions = make(map[string]string)
		}
		snapshot.Decisions[stage] = decision
	})
	rm.persist(r)
	return nil
}

func (rm *RunManager) SetPRURL(id, prURL string) error {
	r, ok := rm.lookupRun(id)
	if !ok {
		return fmt.Errorf("daemon: no run %q", id)
	}
	r.update(func(snapshot *RunSnapshot) { snapshot.PRURL = prURL })
	rm.persist(r)
	return nil
}

func (rm *RunManager) AddFindings(id string, findings []RunFinding) error {
	r, ok := rm.lookupRun(id)
	if !ok {
		return fmt.Errorf("daemon: no run %q", id)
	}
	r.update(func(snapshot *RunSnapshot) {
		snapshot.Findings = append(snapshot.Findings, findings...)
	})
	rm.persist(r)
	return nil
}

func (rm *RunManager) AppendSubmissionEvent(id string, event SubmissionEvent) error {
	r, ok := rm.lookupRun(id)
	if !ok {
		return fmt.Errorf("daemon: no run %q", id)
	}
	if event.RecordedAt.IsZero() {
		event.RecordedAt = time.Now().UTC()
	}
	r.update(func(snapshot *RunSnapshot) {
		for _, existing := range snapshot.SubmissionEvents {
			if existing.Gate == event.Gate && existing.Ref == event.Ref && existing.InputSHA == event.InputSHA {
				return
			}
		}
		snapshot.SubmissionEvents = append(snapshot.SubmissionEvents, event)
	})
	rm.persist(r)
	return nil
}
