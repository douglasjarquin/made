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

func TestSpawn_CodexUsesStructuredExecContract(t *testing.T) {
	bin := agenttest.Build(t)
	worktree := t.TempDir()
	scenarioPath := filepath.Join(t.TempDir(), "scenario.json")
	if err := os.WriteFile(scenarioPath, []byte(`{"findings":[]}`), 0o644); err != nil {
		t.Fatalf("write scenario: %v", err)
	}
	logPath := filepath.Join(t.TempDir(), "agent.log")

	if _, err := agent.Spawn(context.Background(), agent.KindCodex, agent.SpawnParams{
		WorktreePath: worktree,
		BinaryPath:   bin,
		ExtraEnv: []string{
			"FAKE_AGENT_KIND=codex",
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
	for _, token := range []string{"exec", "--json", "--output-schema", "--ephemeral", "-C", worktree} {
		if !strings.Contains(string(data), token) {
			t.Fatalf("expected Codex structured invocation token %q, got %s", token, data)
		}
	}
}

func TestSpawn_DoesNotPassSensitiveEnvironmentToCodex(t *testing.T) {
	bin := agenttest.Build(t)
	worktree := t.TempDir()
	scenarioPath := filepath.Join(t.TempDir(), "scenario.json")
	if err := os.WriteFile(scenarioPath, []byte(`{"findings":[]}`), 0o644); err != nil {
		t.Fatalf("write scenario: %v", err)
	}

	if _, err := agent.Spawn(context.Background(), agent.KindCodex, agent.SpawnParams{
		WorktreePath: worktree,
		BinaryPath:   bin,
		ExtraEnv: []string{
			"FAKE_AGENT_KIND=codex",
			"FAKE_AGENT_SCENARIO=" + scenarioPath,
			"MADE_TEST_SECRET=must-not-reach-review-agent",
			"DATABASE_URL=must-not-reach-review-agent",
			"COOKIE=must-not-reach-review-agent",
			"JWT_KEY=must-not-reach-review-agent",
			"KUBECONFIG=/must-not-reach-review-agent",
		},
	}); err != nil {
		t.Fatalf("Spawn exposed sensitive environment: %v", err)
	}
}

func TestSpawn_RejectsStructuredOutputWithoutFindingsField(t *testing.T) {
	bin := agenttest.Build(t)
	scenarioPath := filepath.Join(t.TempDir(), "invalid.json")
	if err := os.WriteFile(scenarioPath, []byte(`{"unexpected":[]}`), 0o644); err != nil {
		t.Fatalf("write invalid scenario: %v", err)
	}

	_, err := agent.Spawn(context.Background(), agent.KindCodex, agent.SpawnParams{
		WorktreePath: t.TempDir(),
		BinaryPath:   bin,
		ExtraEnv: []string{
			"FAKE_AGENT_KIND=codex",
			"FAKE_AGENT_SCENARIO=" + scenarioPath,
		},
	})
	if err == nil {
		t.Fatal("expected schema-invalid structured output to fail closed")
	}
}

func TestFindingsJSONRoundTripUsesArrayShape(t *testing.T) {
	data, err := json.Marshal(agent.Findings{Findings: []agent.Finding{}})
	if err != nil {
		t.Fatalf("marshal findings: %v", err)
	}
	if string(data) != `{"findings":[]}` {
		t.Fatalf("unexpected structured findings shape: %s", data)
	}
}
