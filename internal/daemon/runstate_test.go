package daemon

import (
	"context"
	"reflect"
	"testing"
	"time"
)

func TestRunManager_UpdateStagesVisibleViaSnapshot(t *testing.T) {
	rm := NewRunManager()
	id := rm.NewRunID()

	release := make(chan struct{})
	if _, err := rm.Submit(id, "gate-repo-stages", "main", func(ctx context.Context, emit func(Event)) error {
		<-release
		return nil
	}); err != nil {
		t.Fatalf("submit run: %v", err)
	}
	defer close(release)

	stages := []StageResult{
		{Name: "intent", Result: "pass"},
		{Name: "rebase", Result: "pass"},
	}
	if err := rm.UpdateStages(id, stages); err != nil {
		t.Fatalf("UpdateStages: %v", err)
	}

	snap, ok := rm.Snapshot(id)
	if !ok {
		t.Fatal("snapshot not found")
	}
	if len(snap.Stages) != 2 {
		t.Fatalf("Stages = %+v, want 2 entries", snap.Stages)
	}
	if !reflect.DeepEqual(snap.Stages, stages) {
		t.Errorf("Stages = %+v, want %+v", snap.Stages, stages)
	}
}

func TestRunManager_UpdatePendingFindingsVisibleViaSnapshot(t *testing.T) {
	rm := NewRunManager()
	id := rm.NewRunID()

	release := make(chan struct{})
	if _, err := rm.Submit(id, "gate-repo-findings", "main", func(ctx context.Context, emit func(Event)) error {
		<-release
		return nil
	}); err != nil {
		t.Fatalf("submit run: %v", err)
	}
	defer close(release)

	findings := []AskUserFinding{
		{Stage: "review", Message: "Should this helper be exported?"},
	}
	if err := rm.UpdatePendingFindings(id, findings); err != nil {
		t.Fatalf("UpdatePendingFindings: %v", err)
	}

	snap, ok := rm.Snapshot(id)
	if !ok {
		t.Fatal("snapshot not found")
	}
	if len(snap.PendingFindings) != 1 || snap.PendingFindings[0] != findings[0] {
		t.Errorf("PendingFindings = %+v, want %+v", snap.PendingFindings, findings)
	}
}

func TestRunManager_UpdateStagesUnknownRunFails(t *testing.T) {
	rm := NewRunManager()
	if err := rm.UpdateStages("no-such-run", []StageResult{{Name: "intent", Result: "pass"}}); err == nil {
		t.Fatal("expected error updating stages for unknown run")
	}
}

func TestRunManager_UpdatePendingFindingsUnknownRunFails(t *testing.T) {
	rm := NewRunManager()
	if err := rm.UpdatePendingFindings("no-such-run", []AskUserFinding{{Stage: "review", Message: "x"}}); err == nil {
		t.Fatal("expected error updating pending findings for unknown run")
	}
}

func TestRunManager_UpdateStagesReflectsListToo(t *testing.T) {
	rm := NewRunManager()
	id := rm.NewRunID()

	if _, err := rm.Submit(id, "gate-repo-list", "main", func(ctx context.Context, emit func(Event)) error {
		return nil
	}); err != nil {
		t.Fatalf("submit run: %v", err)
	}

	deadline := time.After(2 * time.Second)
	for {
		snap, ok := rm.Snapshot(id)
		if ok && (snap.Status == RunSucceeded || snap.Status == RunFailed) {
			break
		}
		select {
		case <-deadline:
			t.Fatal("timed out waiting for run to complete")
		case <-time.After(5 * time.Millisecond):
		}
	}

	stages := []StageResult{{Name: "intent", Result: "pass"}}
	if err := rm.UpdateStages(id, stages); err != nil {
		t.Fatalf("UpdateStages: %v", err)
	}

	runs := rm.List()
	found := false
	for _, r := range runs {
		if r.ID == id {
			found = true
			if len(r.Stages) != 1 || !reflect.DeepEqual(r.Stages[0], stages[0]) {
				t.Errorf("List() Stages = %+v, want %+v", r.Stages, stages)
			}
		}
	}
	if !found {
		t.Fatal("run not found in List()")
	}
}
