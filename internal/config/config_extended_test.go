package config

import (
	"strings"
	"testing"

	"github.com/douglasjarquin/made/internal/agent"
	"github.com/douglasjarquin/made/internal/github"
)

const trustedFixtureWithEvidenceModeAndBudget = `
review:
  required: true
ci:
  required: true
  rerun_budget: 5
test:
  evidence:
    branch: trusted-evidence-branch
    store_in_repo: true
    dir: .made/evidence
commands:
  test: trusted-test-cmd
  lint: trusted-lint-cmd
agent: trusted-agent
allow_repo_commands: false
`

func TestLoadEffectiveConfig_EvidenceStoreInRepoAndDirAlwaysFromTrusted(t *testing.T) {
	dir := t.TempDir()
	trustedPath := writeConfigFile(t, dir, "trusted.yaml", trustedFixtureWithEvidenceModeAndBudget)
	pushedPath := writeConfigFile(t, dir, "pushed.yaml", pushedFixture)

	cfg, err := LoadEffectiveConfig(trustedPath, pushedPath)
	if err != nil {
		t.Fatalf("LoadEffectiveConfig returned unexpected error: %v", err)
	}

	if !cfg.Test.Evidence.StoreInRepo {
		t.Errorf("Test.Evidence.StoreInRepo = false, want true (trusted copy value)")
	}
	if cfg.Test.Evidence.Dir != ".made/evidence" {
		t.Errorf("Test.Evidence.Dir = %q, want %q (trusted copy value)", cfg.Test.Evidence.Dir, ".made/evidence")
	}
}

func TestCI_RerunBudgetDefaultsToTwoWhenUnset(t *testing.T) {
	dir := t.TempDir()
	trustedPath := writeConfigFile(t, dir, "trusted.yaml", trustedFixture)
	pushedPath := writeConfigFile(t, dir, "pushed.yaml", pushedFixture)

	cfg, err := LoadEffectiveConfig(trustedPath, pushedPath)
	if err != nil {
		t.Fatalf("LoadEffectiveConfig returned unexpected error: %v", err)
	}

	if cfg.CI.RerunBudget != 2 {
		t.Errorf("CI.RerunBudget = %d, want 2 (default when unset)", cfg.CI.RerunBudget)
	}
}

func TestCI_RerunBudgetHonorsExplicitValue(t *testing.T) {
	dir := t.TempDir()
	trustedPath := writeConfigFile(t, dir, "trusted.yaml", trustedFixtureWithEvidenceModeAndBudget)
	pushedPath := writeConfigFile(t, dir, "pushed.yaml", pushedFixture)

	cfg, err := LoadEffectiveConfig(trustedPath, pushedPath)
	if err != nil {
		t.Fatalf("LoadEffectiveConfig returned unexpected error: %v", err)
	}

	if cfg.CI.RerunBudget != 5 {
		t.Errorf("CI.RerunBudget = %d, want 5 (explicit trusted copy value)", cfg.CI.RerunBudget)
	}
}

func TestCI_CheckScopeCanBeConfiguredAsAll(t *testing.T) {
	dir := t.TempDir()
	trustedPath := writeConfigFile(t, dir, "trusted.yaml", `version: 1
ci:
  required: true
  check_scope: all
`)

	cfg, err := LoadEffectiveConfig(trustedPath, "")
	if err != nil {
		t.Fatalf("LoadEffectiveConfig rejected configured CI check scope: %v", err)
	}
	if cfg.CI.CheckScope != github.CheckScopeAll {
		t.Fatalf("CI.CheckScope = %q, want %q", cfg.CI.CheckScope, github.CheckScopeAll)
	}
}

func TestCI_CheckScopeDefaultsToRequired(t *testing.T) {
	dir := t.TempDir()
	trustedPath := writeConfigFile(t, dir, "trusted.yaml", trustedFixture)

	cfg, err := LoadEffectiveConfig(trustedPath, "")
	if err != nil {
		t.Fatalf("LoadEffectiveConfig: %v", err)
	}
	if cfg.CI.CheckScope != github.CheckScopeRequired {
		t.Fatalf("CI.CheckScope = %q, want %q", cfg.CI.CheckScope, github.CheckScopeRequired)
	}
}

func TestConfig_TestCommandTokenizesNonEmptyString(t *testing.T) {
	cfg := Config{Commands: Commands{Test: "go test ./..."}}

	got := cfg.TestCommand()
	want := []string{"sh", "-c", "go test ./..."}

	if len(got) != len(want) {
		t.Fatalf("TestCommand() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("TestCommand() = %v, want %v", got, want)
		}
	}
}

func TestConfig_TestCommandReturnsNilWhenEmpty(t *testing.T) {
	cfg := Config{}

	if got := cfg.TestCommand(); got != nil {
		t.Errorf("TestCommand() = %v, want nil for empty Commands.Test", got)
	}
}

func TestConfig_LintCommandTokenizesNonEmptyString(t *testing.T) {
	cfg := Config{Commands: Commands{Lint: "golangci-lint run"}}

	got := cfg.LintCommand()
	want := []string{"sh", "-c", "golangci-lint run"}

	if len(got) != len(want) {
		t.Fatalf("LintCommand() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("LintCommand() = %v, want %v", got, want)
		}
	}
}

func TestConfig_LintCommandReturnsNilWhenEmpty(t *testing.T) {
	cfg := Config{}

	if got := cfg.LintCommand(); got != nil {
		t.Errorf("LintCommand() = %v, want nil for empty Commands.Lint", got)
	}
}

func TestConfig_AgentKindMapsClaude(t *testing.T) {
	cfg := Config{Agent: "claude"}

	kind, err := cfg.AgentKind()
	if err != nil {
		t.Fatalf("AgentKind() returned unexpected error: %v", err)
	}
	if kind != agent.KindClaude {
		t.Errorf("AgentKind() = %v, want %v", kind, agent.KindClaude)
	}
}

func TestConfig_AgentKindMapsCodex(t *testing.T) {
	cfg := Config{Agent: "codex"}

	kind, err := cfg.AgentKind()
	if err != nil {
		t.Fatalf("AgentKind() returned unexpected error: %v", err)
	}
	if kind != agent.KindCodex {
		t.Errorf("AgentKind() = %v, want %v", kind, agent.KindCodex)
	}
}

func TestConfig_AgentKindFailsClosedOnEmpty(t *testing.T) {
	cfg := Config{Agent: ""}

	_, err := cfg.AgentKind()
	if err == nil {
		t.Fatalf("AgentKind() returned nil error for empty Agent; want fail-closed error")
	}
}

func TestConfig_AgentKindFailsClosedOnUnrecognizedValue(t *testing.T) {
	cfg := Config{Agent: "gpt4"}

	_, err := cfg.AgentKind()
	if err == nil {
		t.Fatalf("AgentKind() returned nil error for unrecognized agent %q; want fail-closed error", "gpt4")
	}
	if !strings.Contains(err.Error(), "gpt4") {
		t.Errorf("AgentKind() error = %q, want it to name the invalid value %q", err.Error(), "gpt4")
	}
}
