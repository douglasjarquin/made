package daemon

import (
	"fmt"
	"slices"
)

type StageResult struct {
	Name         string   `json:"name"`
	Result       string   `json:"result"`
	Message      string   `json:"message,omitempty"`
	Error        string   `json:"error,omitempty"`
	EvidenceRefs []string `json:"evidence_refs,omitempty"`
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
		s.Stages = cloneStageResults(stages)
		s.CurrentStage = currentStage(s.Stages)
	})
	return rm.persistRun(r)
}

func (rm *RunManager) UpdatePendingFindings(id string, findings []AskUserFinding) error {
	r, ok := rm.lookupRun(id)
	if !ok {
		return fmt.Errorf("daemon: no run %q", id)
	}
	r.update(func(s *RunSnapshot) {
		s.PendingFindings = append([]AskUserFinding(nil), findings...)
	})
	return rm.persistRun(r)
}

func (rm *RunManager) SetCurrentStage(id, stage string) error {
	r, ok := rm.lookupRun(id)
	if !ok {
		return fmt.Errorf("daemon: no run %q", id)
	}
	r.update(func(s *RunSnapshot) {
		s.CurrentStage = stage
	})
	return rm.persistRun(r)
}

func (rm *RunManager) AddEvidenceRef(id, ref string) error {
	r, ok := rm.lookupRun(id)
	if !ok {
		return fmt.Errorf("daemon: no run %q", id)
	}
	r.update(func(s *RunSnapshot) {
		if !slices.Contains(s.EvidenceRefs, ref) {
			s.EvidenceRefs = append(s.EvidenceRefs, ref)
		}
	})
	return rm.persistRun(r)
}

func (rm *RunManager) UpdateSubmissionOutput(id, outputSHA string) error {
	r, ok := rm.lookupRun(id)
	if !ok {
		return fmt.Errorf("daemon: no run %q", id)
	}
	r.update(func(s *RunSnapshot) {
		s.OutputSHA = outputSHA
	})
	return rm.persistRun(r)
}

func cloneStageResults(stages []StageResult) []StageResult {
	out := append([]StageResult(nil), stages...)
	for i := range out {
		out[i].EvidenceRefs = append([]string(nil), stages[i].EvidenceRefs...)
	}
	return out
}

func currentStage(stages []StageResult) string {
	for _, stage := range stages {
		if stage.Result != "pass" {
			return stage.Name
		}
	}
	return ""
}

func (rm *RunManager) lookupRun(id string) (*run, bool) {
	rm.mu.Lock()
	r, ok := rm.runs[id]
	rm.mu.Unlock()
	return r, ok
}
