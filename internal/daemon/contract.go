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
	if stage == "" || decision == "" {
		return fmt.Errorf("daemon: decision stage and value are required")
	}
	return rm.updateRun(id, func(snapshot *RunSnapshot) error {
		if snapshot.Decisions == nil {
			snapshot.Decisions = make(map[string]string)
		}
		snapshot.Decisions[stage] = decision
		return nil
	})
}

func (rm *RunManager) SetPRURL(id, prURL string) error {
	return rm.updateRun(id, func(snapshot *RunSnapshot) error {
		snapshot.PRURL = prURL
		return nil
	})
}

func (rm *RunManager) SetOutputSHA(id, outputSHA string) error {
	if outputSHA == "" {
		return fmt.Errorf("daemon: output SHA is required")
	}
	return rm.updateRun(id, func(snapshot *RunSnapshot) error {
		snapshot.OutputSHA = outputSHA
		return nil
	})
}

func (rm *RunManager) AddFindings(id string, findings []RunFinding) error {
	return rm.updateRun(id, func(snapshot *RunSnapshot) error {
		snapshot.Findings = append(snapshot.Findings, findings...)
		return nil
	})
}

func (rm *RunManager) AppendSubmissionEvent(id string, event SubmissionEvent) error {
	if event.RecordedAt.IsZero() {
		event.RecordedAt = time.Now().UTC()
	}
	return rm.updateRun(id, func(snapshot *RunSnapshot) error {
		for _, existing := range snapshot.SubmissionEvents {
			if existing.Gate == event.Gate && existing.Ref == event.Ref && existing.InputSHA == event.InputSHA {
				return nil
			}
		}
		snapshot.SubmissionEvents = append(snapshot.SubmissionEvents, event)
		return nil
	})
}

func (rm *RunManager) updateRun(id string, update func(*RunSnapshot) error) error {
	r, ok := rm.lookupRun(id)
	if !ok {
		return fmt.Errorf("daemon: no run %q", id)
	}
	r.persistMu.Lock()
	defer r.persistMu.Unlock()
	candidate := r.snapshot()
	if err := update(&candidate); err != nil {
		return err
	}
	if err := rm.persistAndReplace(r, candidate); err != nil {
		return fmt.Errorf("persist run %q: %w", id, err)
	}
	return nil
}
