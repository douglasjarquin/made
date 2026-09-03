package managed_test

import (
	"context"
	"testing"

	"github.com/douglasjarquin/made/internal/agent"
	"github.com/douglasjarquin/made/internal/agent/agenttest"
	"github.com/douglasjarquin/made/internal/managed"
)

// stagePlanOptions wires a fake agent binary in when configContent configures one, so Review can run without a real Codex install.
func stagePlanOptions(t *testing.T, runID, missionID, configContent string) *managed.Options {
	t.Helper()
	workspace, baseSHA, inputSHA := makeTestRepo(t)
	configPath, policyHash := makeTrustedConfig(t, configContent)
	evidenceDir := makeEvidenceDir(t)

	opts := &managed.Options{
		RunID:         runID,
		MissionID:     missionID,
		Workspace:     workspace,
		BaseSHA:       baseSHA,
		InputSHA:      inputSHA,
		TrustedConfig: configPath,
		PolicyHash:    policyHash,
		EvidenceDir:   evidenceDir,
	}
	if containsAgentCodex(configContent) {
		scenario := writeScenario(t, agent.Findings{Findings: []agent.Finding{}})
		opts.ReviewSource = "internal"
		opts.ReviewAgentBinaryPath = agenttest.Build(t)
		opts.ReviewAgentExtraEnv = []string{"FAKE_AGENT_SCENARIO=" + scenario}
	}
	return opts
}

func containsAgentCodex(s string) bool {
	return contains(s, "agent: codex")
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (func() bool {
		for i := 0; i+len(needle) <= len(haystack); i++ {
			if haystack[i:i+len(needle)] == needle {
				return true
			}
		}
		return false
	})()
}

// stageOutcome returns the outcome string reported for a given stage name in
// the run's stage.completed events, or "" if the stage never reported.
func stageOutcome(t *testing.T, events []map[string]any, stage string) string {
	t.Helper()
	for _, ev := range events {
		if ev["event"] != "stage.completed" {
			continue
		}
		payload, ok := ev["payload"].(map[string]any)
		if !ok {
			continue
		}
		if payload["stage"] == stage {
			outcome, _ := payload["outcome"].(string)
			return outcome
		}
	}
	return ""
}

func TestStagePlan_ReviewOnly(t *testing.T) {
	const cfg = `version: 1
review:
  required: true
agent: codex
`
	opts := stagePlanOptions(t, "G-plan-review-only", "M-plan", cfg)
	res := runManaged(t, context.Background(), opts)
	if res.exitCode != 0 {
		t.Fatalf("expected exit 0, got %d (stderr: %s)", res.exitCode, res.stderr)
	}
	if got := stageOutcome(t, res.events, "review"); got != "passed" {
		t.Errorf("review outcome = %q, want passed", got)
	}
	for _, stage := range []string{"test", "lint", "document"} {
		if got := stageOutcome(t, res.events, stage); got != "not_configured" {
			t.Errorf("%s outcome = %q, want not_configured", stage, got)
		}
	}
}

func TestStagePlan_ReviewPlusLintNoTest(t *testing.T) {
	const cfg = `version: 1
review:
  required: true
agent: codex
commands:
  lint: "true"
`
	opts := stagePlanOptions(t, "G-plan-review-lint", "M-plan", cfg)
	res := runManaged(t, context.Background(), opts)
	if res.exitCode != 0 {
		t.Fatalf("expected exit 0, got %d (stderr: %s)", res.exitCode, res.stderr)
	}
	if got := stageOutcome(t, res.events, "review"); got != "passed" {
		t.Errorf("review outcome = %q, want passed", got)
	}
	if got := stageOutcome(t, res.events, "lint"); got != "passed" {
		t.Errorf("lint outcome = %q, want passed", got)
	}
	if got := stageOutcome(t, res.events, "test"); got != "not_configured" {
		t.Errorf("test outcome = %q, want not_configured", got)
	}
}

func TestStagePlan_TestOnly(t *testing.T) {
	const cfg = `version: 1
commands:
  test: "true"
`
	opts := stagePlanOptions(t, "G-plan-test-only", "M-plan", cfg)
	res := runManaged(t, context.Background(), opts)
	if res.exitCode != 0 {
		t.Fatalf("expected exit 0, got %d (stderr: %s)", res.exitCode, res.stderr)
	}
	if got := stageOutcome(t, res.events, "test"); got != "passed" {
		t.Errorf("test outcome = %q, want passed", got)
	}
	for _, stage := range []string{"review", "lint", "document"} {
		if got := stageOutcome(t, res.events, stage); got != "not_configured" {
			t.Errorf("%s outcome = %q, want not_configured", stage, got)
		}
	}
}

func TestStagePlan_LintOnly(t *testing.T) {
	const cfg = `version: 1
commands:
  lint: "true"
`
	opts := stagePlanOptions(t, "G-plan-lint-only", "M-plan", cfg)
	res := runManaged(t, context.Background(), opts)
	if res.exitCode != 0 {
		t.Fatalf("expected exit 0, got %d (stderr: %s)", res.exitCode, res.stderr)
	}
	if got := stageOutcome(t, res.events, "lint"); got != "passed" {
		t.Errorf("lint outcome = %q, want passed", got)
	}
	for _, stage := range []string{"review", "test", "document"} {
		if got := stageOutcome(t, res.events, stage); got != "not_configured" {
			t.Errorf("%s outcome = %q, want not_configured", stage, got)
		}
	}
}

func TestStagePlan_AllConfigured(t *testing.T) {
	const cfg = `version: 1
review:
  required: true
agent: codex
commands:
  test: "true"
  lint: "true"
`
	opts := stagePlanOptions(t, "G-plan-all", "M-plan", cfg)
	res := runManaged(t, context.Background(), opts)
	if res.exitCode != 0 {
		t.Fatalf("expected exit 0, got %d (stderr: %s)", res.exitCode, res.stderr)
	}
	for _, stage := range []string{"review", "test", "lint"} {
		if got := stageOutcome(t, res.events, stage); got != "passed" {
			t.Errorf("%s outcome = %q, want passed", stage, got)
		}
	}
	if got := stageOutcome(t, res.events, "document"); got != "not_configured" {
		t.Errorf("document outcome = %q, want not_configured", got)
	}
}

func TestStagePlan_DocumentAbsentConfiguredDisabled(t *testing.T) {
	const absentCfg = `version: 1
commands:
  test: "true"
`
	opts := stagePlanOptions(t, "G-plan-doc-absent", "M-plan", absentCfg)
	res := runManaged(t, context.Background(), opts)
	if got := stageOutcome(t, res.events, "document"); got != "not_configured" {
		t.Errorf("absent: document outcome = %q, want not_configured", got)
	}

	const configuredCfg = `version: 1
commands:
  test: "true"
document:
  rules:
    - path_pattern: "*.go"
      required_doc_pattern: "docs/**"
`
	opts2 := stagePlanOptions(t, "G-plan-doc-configured", "M-plan", configuredCfg)
	res2 := runManaged(t, context.Background(), opts2)
	if got := stageOutcome(t, res2.events, "document"); got == "not_configured" || got == "" {
		t.Errorf("configured: document outcome = %q, want a real run outcome", got)
	}

	const disabledCfg = `version: 1
commands:
  test: "true"
document:
  rules:
    - path_pattern: "*.go"
      required_doc_pattern: "docs/**"
stages:
  document:
    enabled: false
`
	opts3 := stagePlanOptions(t, "G-plan-doc-disabled", "M-plan", disabledCfg)
	res3 := runManaged(t, context.Background(), opts3)
	if got := stageOutcome(t, res3.events, "document"); got != "disabled" {
		t.Errorf("disabled: document outcome = %q, want disabled", got)
	}
}

func TestStagePlan_TestCommandAbsentNoLanes(t *testing.T) {
	const cfg = `version: 1
commands:
  lint: "true"
`
	opts := stagePlanOptions(t, "G-plan-test-absent", "M-plan", cfg)
	res := runManaged(t, context.Background(), opts)
	if got := stageOutcome(t, res.events, "test"); got != "not_configured" {
		t.Errorf("test outcome = %q, want not_configured", got)
	}
}

func TestStagePlan_NoEffectiveValidationWorkFails(t *testing.T) {
	const cfg = `version: 1
disable_project_settings: true
`
	opts := stagePlanOptions(t, "G-plan-no-work", "M-plan", cfg)
	res := runManaged(t, context.Background(), opts)
	if res.exitCode == 0 {
		t.Fatalf("expected non-zero exit for a run with no effective validation work, got 0")
	}
	if got := terminalOutcome(t, res.events); got == "passed" {
		t.Errorf("expected a run with no effective validation work to never report passed, got %q", got)
	}
}

func TestStagePlan_EveryStageExplicitlyDisabledFails(t *testing.T) {
	const cfg = `version: 1
review:
  required: true
agent: codex
commands:
  test: "true"
  lint: "true"
document:
  rules:
    - path_pattern: "*.go"
      required_doc_pattern: "docs/**"
stages:
  review:
    enabled: false
  test:
    enabled: false
  lint:
    enabled: false
  document:
    enabled: false
`
	opts := stagePlanOptions(t, "G-plan-all-disabled", "M-plan", cfg)
	res := runManaged(t, context.Background(), opts)
	if res.exitCode == 0 {
		t.Fatalf("expected non-zero exit when every stage is disabled, got 0")
	}
	for _, stage := range []string{"review", "test", "lint", "document"} {
		if got := stageOutcome(t, res.events, stage); got != "disabled" {
			t.Errorf("%s outcome = %q, want disabled", stage, got)
		}
	}
	if got := terminalOutcome(t, res.events); got == "passed" {
		t.Errorf("expected a fully-disabled run to never report passed, got %q", got)
	}
}
