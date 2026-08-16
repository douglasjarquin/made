package agent_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/douglasjarquin/made/internal/agent"
	"github.com/douglasjarquin/made/internal/agent/agenttest"
)

func writeScenario(t *testing.T, findings agent.Findings) string {
	t.Helper()
	data, err := json.Marshal(findings)
	if err != nil {
		t.Fatalf("marshal scenario: %v", err)
	}
	path := filepath.Join(t.TempDir(), "scenario.json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write scenario: %v", err)
	}
	return path
}

func TestSpawn_ParsesFindingsFromFakeAgent(t *testing.T) {
	bin := agenttest.Build(t)
	scenarioPath := writeScenario(t, agent.Findings{
		Findings: []agent.Finding{
			{Kind: agent.FindingAutoFixable, Description: "fix formatting", Patch: "diff --git a/x b/x\n", Paths: []string{"x"}},
			{Kind: agent.FindingAskUser, Description: "consider renaming Foo"},
		},
	})

	findings, err := agent.Spawn(context.Background(), agent.KindClaude, agent.SpawnParams{
		WorktreePath: t.TempDir(),
		BinaryPath:   bin,
		ExtraEnv:     []string{"FAKE_AGENT_SCENARIO=" + scenarioPath},
	})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	if len(findings.Findings) != 2 {
		t.Fatalf("expected 2 findings, got %+v", findings.Findings)
	}
	if findings.Findings[0].Kind != agent.FindingAutoFixable {
		t.Fatalf("expected first finding auto-fixable, got %q", findings.Findings[0].Kind)
	}
	if findings.Findings[1].Kind != agent.FindingAskUser {
		t.Fatalf("expected second finding ask-user, got %q", findings.Findings[1].Kind)
	}
}

func TestSpawn_NonZeroExitReturnsError(t *testing.T) {
	bin := agenttest.Build(t)

	_, err := agent.Spawn(context.Background(), agent.KindCodex, agent.SpawnParams{
		WorktreePath: t.TempDir(),
		BinaryPath:   bin,
		ExtraEnv:     []string{"FAKE_AGENT_EXIT_CODE=1"},
	})
	if err == nil {
		t.Fatal("expected an error for a non-zero fakeagent exit")
	}
	if !strings.Contains(err.Error(), "codex") {
		t.Fatalf("expected error to name the agent kind, got %v", err)
	}
}

func TestSpawn_LogsInvocation(t *testing.T) {
	bin := agenttest.Build(t)
	scenarioPath := writeScenario(t, agent.Findings{})
	logPath := filepath.Join(t.TempDir(), "invocations.log")

	if _, err := agent.Spawn(context.Background(), agent.KindClaude, agent.SpawnParams{
		WorktreePath: t.TempDir(),
		BinaryPath:   bin,
		ExtraEnv: []string{
			"FAKE_AGENT_SCENARIO=" + scenarioPath,
			"FAKE_AGENT_LOG_FILE=" + logPath,
		},
	}); err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read invocation log: %v", err)
	}
	if !strings.Contains(string(data), "invoked:") {
		t.Fatalf("expected invocation log entry, got %q", data)
	}
}
