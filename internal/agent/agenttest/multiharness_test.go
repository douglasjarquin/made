package agenttest_test

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/douglasjarquin/made/internal/agent"
	"github.com/douglasjarquin/made/internal/agent/agenttest"
)

func TestBuildFleet_PresenceOnlyConfiguredKindsAreOnPath(t *testing.T) {
	agenttest.BuildFleet(t, map[agent.Kind]agenttest.FleetOptions{
		agent.KindClaude: {AuthExitCode: 0},
	})

	if _, err := exec.LookPath("claude"); err != nil {
		t.Errorf("LookPath(claude) = %v, want present", err)
	}
	if _, err := exec.LookPath("codex"); err == nil {
		t.Errorf("LookPath(codex) succeeded, want not-found (not included in fleet)")
	}
}

func TestBuildFleet_AuthStatusExitCodeHonored(t *testing.T) {
	agenttest.BuildFleet(t, map[agent.Kind]agenttest.FleetOptions{
		agent.KindClaude: {AuthExitCode: 1},
		agent.KindCodex:  {AuthExitCode: 0},
	})

	if err := exec.Command("claude", "auth", "status").Run(); err == nil {
		t.Errorf("claude auth status succeeded, want nonzero exit (AuthExitCode: 1)")
	}
	if err := exec.Command("codex", "login", "status").Run(); err != nil {
		t.Errorf("codex login status = %v, want exit 0 (AuthExitCode: 0)", err)
	}
}

func TestBuildFleet_ReviewInvocationProducesFindingsEnvelope(t *testing.T) {
	agenttest.BuildFleet(t, map[agent.Kind]agenttest.FleetOptions{
		agent.KindClaude: {},
	})

	out, err := exec.Command("claude", "-p", "--strict-mcp-config", "--no-session-persistence",
		"--output-format", "json", "--permission-mode", "plan",
		"--json-schema", `{"properties":{"findings":{}}}`).Output()
	if err != nil {
		t.Fatalf("claude review invocation failed: %v", err)
	}
	if !strings.Contains(string(out), `"type":"result"`) {
		t.Errorf("output = %s, want a claude-shaped result envelope", out)
	}
}

func TestBuildFleet_ReviewInvocationStderrOnFailure(t *testing.T) {
	agenttest.BuildFleet(t, map[agent.Kind]agenttest.FleetOptions{
		agent.KindClaude: {ExitCode: 1, Stderr: "Claude usage limit reached, try again later"},
	})

	cmd := exec.Command("claude", "-p", "--strict-mcp-config", "--no-session-persistence",
		"--output-format", "json", "--permission-mode", "plan",
		"--json-schema", `{"properties":{"findings":{}}}`)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err == nil {
		t.Fatalf("claude review invocation succeeded, want exit 1")
	}
	if !strings.Contains(stderr.String(), "usage limit reached") {
		t.Errorf("stderr = %q, want it to contain the scripted capacity message", stderr.String())
	}
}
