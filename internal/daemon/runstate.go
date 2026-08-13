package daemon

import "fmt"

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
		s.Stages = stages
	})
	return nil
}

func (rm *RunManager) UpdatePendingFindings(id string, findings []AskUserFinding) error {
	r, ok := rm.lookupRun(id)
	if !ok {
		return fmt.Errorf("daemon: no run %q", id)
	}
	r.update(func(s *RunSnapshot) {
		s.PendingFindings = findings
	})
	return nil
}

func (rm *RunManager) lookupRun(id string) (*run, bool) {
	rm.mu.Lock()
	r, ok := rm.runs[id]
	rm.mu.Unlock()
	return r, ok
}
