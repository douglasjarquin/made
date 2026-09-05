package verify_test

import (
	"testing"

	"github.com/douglasjarquin/made/internal/agent"
	"github.com/douglasjarquin/made/internal/managed"
	"github.com/douglasjarquin/made/internal/verify"
)

func TestBuildReceipt_CopiesAgentResolutionFromStageResult(t *testing.T) {
	claude := agent.KindClaude
	res := verify.EngineResult{
		Outcome: managed.OutcomePassed,
		StageResults: []managed.StageResult{
			{Stage: "review", Outcome: managed.OutcomePassed, AgentResolution: &agent.AgentResolution{
				Selected: &claude,
				Attempts: []agent.CandidateAttempt{{Kind: agent.KindCodex, Reason: agent.ReasonMissing}},
			}},
			{Stage: "test", Outcome: managed.OutcomePassed},
		},
	}
	r := verify.BuildReceipt(t.TempDir(), "base123", "input456", verify.ConfigIdentity{}, nil, res)

	var reviewReceipt, testReceipt *verify.StageReceipt
	for i := range r.Stages {
		switch r.Stages[i].Name {
		case "review":
			reviewReceipt = &r.Stages[i]
		case "test":
			testReceipt = &r.Stages[i]
		}
	}
	if reviewReceipt == nil || reviewReceipt.AgentResolution == nil || reviewReceipt.AgentResolution.Selected == nil || *reviewReceipt.AgentResolution.Selected != agent.KindClaude {
		t.Fatalf("review StageReceipt.AgentResolution = %+v, want Selected=claude", reviewReceipt)
	}
	if testReceipt == nil || testReceipt.AgentResolution != nil {
		t.Errorf("test StageReceipt.AgentResolution = %+v, want nil (only review carries this)", testReceipt)
	}
}

func TestReceiptSchemaVersion_UnchangedByAgentResolutionField(t *testing.T) {
	if verify.ReceiptSchemaVersion != 3 {
		t.Errorf("ReceiptSchemaVersion = %d, want 3 (additive field must not bump this, matching issue #61's precedent)", verify.ReceiptSchemaVersion)
	}
}

func TestManagedSchemaVersion_UnchangedByAgentResolutionField(t *testing.T) {
	if managed.SchemaVersion != 1 {
		t.Errorf("managed.SchemaVersion = %d, want 1 (additive field must not bump this)", managed.SchemaVersion)
	}
}
