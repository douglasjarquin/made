package agenttest

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/douglasjarquin/made/internal/agent"
)

// FleetOptions scripts one candidate kind's behavior within a BuildFleet call.
type FleetOptions struct {
	// AuthExitCode is the exit code for this kind's auth-status probe
	// (codex login status / claude auth status). 0 means authenticated.
	// Ignored for cursor/grok, which have no such probe (project: agent
	// auto-resolve, decision D2).
	AuthExitCode int
	// ExitCode is the exit code for a real review invocation. 0 means
	// success; the response is a valid findings envelope in that case.
	ExitCode int
	// Stderr is emitted when ExitCode != 0.
	Stderr string
	// Findings is the raw JSON findings payload for a successful (ExitCode
	// == 0) review invocation. Defaults to an empty agent.Findings{}.
	Findings string
}

// BuildFleet compiles the fakeagent test double once and places it under
// each requested kind's real binary name (Kind.BinaryName()) on a scoped
// PATH, so exec.LookPath and per-kind auth-status probes see an independent,
// differently-named CLI per candidate - a kind absent from entries is simply
// not on that PATH, so its lookup fails naturally. Sets PATH and
// FAKE_AGENT_FLEET_CONFIG via t.Setenv, so callers just invoke the real
// per-kind commands (e.g. exec.LookPath("claude")) after calling this.
func BuildFleet(t *testing.T, entries map[agent.Kind]FleetOptions) string {
	t.Helper()
	src := Build(t)
	source, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("agenttest: read compiled fakeagent: %v", err)
	}

	dir := t.TempDir()
	fleet := make(map[string]fleetEntry, len(entries))
	for kind, opts := range entries {
		name := kind.BinaryName()
		dest := filepath.Join(dir, name)
		if err := os.WriteFile(dest, source, 0o755); err != nil {
			t.Fatalf("agenttest: write fleet binary %s: %v", name, err)
		}
		findings := opts.Findings
		if findings == "" {
			data, err := json.Marshal(agent.Findings{})
			if err != nil {
				t.Fatalf("agenttest: marshal default findings: %v", err)
			}
			findings = string(data)
		}
		scenarioPath := filepath.Join(dir, name+".scenario.json")
		if err := os.WriteFile(scenarioPath, []byte(findings), 0o644); err != nil {
			t.Fatalf("agenttest: write fleet scenario %s: %v", name, err)
		}
		fleet[name] = fleetEntry{
			Kind:         string(kind),
			AuthExitCode: opts.AuthExitCode,
			ExitCode:     opts.ExitCode,
			Stderr:       opts.Stderr,
			ScenarioFile: scenarioPath,
		}
	}

	configData, err := json.Marshal(fleet)
	if err != nil {
		t.Fatalf("agenttest: marshal fleet config: %v", err)
	}
	configPath := filepath.Join(dir, "fleet.json")
	if err := os.WriteFile(configPath, configData, 0o644); err != nil {
		t.Fatalf("agenttest: write fleet config: %v", err)
	}

	// PATH is fully replaced, not extended: a real CLI installed on the host
	// running the test (e.g. a dev machine's own authenticated codex) must
	// never leak through and defeat a "kind is missing" scenario.
	t.Setenv("PATH", dir)
	t.Setenv("FAKE_AGENT_FLEET_CONFIG", configPath)
	return dir
}

// fleetEntry mirrors internal/agent/testdata/fakeagent's own fleetEntry -
// duplicated rather than imported since a `package main` command cannot be
// imported as a library.
type fleetEntry struct {
	Kind         string `json:"kind"`
	AuthExitCode int    `json:"auth_exit_code"`
	ExitCode     int    `json:"exit_code"`
	Stderr       string `json:"stderr"`
	ScenarioFile string `json:"scenario_file"`
}
