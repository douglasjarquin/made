package main

import (
	"io"
	"os"
	"strings"
	"testing"

	"github.com/douglasjarquin/made/internal/agent"
	"github.com/douglasjarquin/made/internal/managed"
	"github.com/douglasjarquin/made/internal/verify"
)

func captureHumanReceipt(t *testing.T, r verify.Receipt) string {
	t.Helper()
	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	done := make(chan string, 1)
	go func() {
		data, _ := io.ReadAll(pr)
		done <- string(data)
	}()
	printHumanReceipt(pw, r, "")
	_ = pw.Close()
	return <-done
}

func TestPrintHumanReceipt_NamesResolvedAgent(t *testing.T) {
	claude := agent.KindClaude
	out := captureHumanReceipt(t, verify.Receipt{
		Outcome: managed.OutcomePassed,
		Stages: []verify.StageReceipt{
			{Name: "review", Status: "passed", AgentResolution: &agent.AgentResolution{Selected: &claude}},
		},
	})
	if !strings.Contains(out, "claude") || !strings.Contains(out, "resolved") {
		t.Fatalf("expected human output to name the resolved agent, got:\n%s", out)
	}
}

func TestPrintHumanReceipt_ListsAllExhaustedReasons(t *testing.T) {
	out := captureHumanReceipt(t, verify.Receipt{
		Outcome: managed.OutcomeInfrastructureError,
		Stages: []verify.StageReceipt{
			{Name: "review", Status: "infrastructure_error", AgentResolution: &agent.AgentResolution{
				Attempts: []agent.CandidateAttempt{
					{Kind: agent.KindCodex, Reason: agent.ReasonMissing},
					{Kind: agent.KindClaude, Reason: agent.ReasonUnauthenticated},
				},
			}},
		},
	})
	if !strings.Contains(out, "codex") || !strings.Contains(out, "missing") || !strings.Contains(out, "claude") || !strings.Contains(out, "unauthenticated") {
		t.Fatalf("expected human output to list every candidate and reason, got:\n%s", out)
	}
}

func TestPrintHumanReceipt_NoAgentLineWhenResolutionAbsent(t *testing.T) {
	out := captureHumanReceipt(t, verify.Receipt{
		Outcome: managed.OutcomePassed,
		Stages: []verify.StageReceipt{
			{Name: "test", Status: "passed"},
		},
	})
	if strings.Contains(out, "agent:") {
		t.Fatalf("expected no agent line for a stage with nil AgentResolution, got:\n%s", out)
	}
}
