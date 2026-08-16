package daemon

import "fmt"

func cloneSnapshot(snapshot RunSnapshot) RunSnapshot {
	snapshot.Errors = append([]string(nil), snapshot.Errors...)
	snapshot.Findings = append([]RunFinding(nil), snapshot.Findings...)
	snapshot.Stages = append([]StageResult(nil), snapshot.Stages...)
	snapshot.PendingFindings = append([]AskUserFinding(nil), snapshot.PendingFindings...)
	snapshot.SubmissionEvents = append([]SubmissionEvent(nil), snapshot.SubmissionEvents...)
	if snapshot.Decisions != nil {
		original := snapshot.Decisions
		snapshot.Decisions = make(map[string]string, len(original))
		for key, value := range original {
			snapshot.Decisions[key] = value
		}
	}
	return snapshot
}

type StageResult struct {
	Name   string `json:"name"`
	Result string `json:"result"`
}

type AskUserFinding struct {
	Stage   string `json:"stage"`
	Message string `json:"message"`
}

func (rm *RunManager) UpdateStages(id string, stages []StageResult) error {
	r, ok := rm.lookupRun(id)
	if !ok {
		return fmt.Errorf("daemon: no run %q", id)
	}
	r.update(func(s *RunSnapshot) {
		s.Stages = append([]StageResult(nil), stages...)
	})
	rm.persist(r)
	return nil
}

func (rm *RunManager) UpdatePendingFindings(id string, findings []AskUserFinding) error {
	r, ok := rm.lookupRun(id)
	if !ok {
		return fmt.Errorf("daemon: no run %q", id)
	}
	r.update(func(s *RunSnapshot) {
		s.PendingFindings = append([]AskUserFinding(nil), findings...)
		if len(findings) > 0 && s.Status == RunRunning {
			s.Status = RunAwaitingReview
		}
		if len(findings) == 0 && s.Status == RunAwaitingReview {
			s.Status = RunRunning
		}
	})
	rm.persist(r)
	return nil
}

func (rm *RunManager) lookupRun(id string) (*run, bool) {
	rm.mu.Lock()
	r, ok := rm.runs[id]
	rm.mu.Unlock()
	return r, ok
}
