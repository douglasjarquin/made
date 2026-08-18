package config

import (
	"os"
	"path/filepath"
	"testing"
)

func writeConfigFile(t *testing.T, dir, name, contents string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("writing config fixture %s: %v", path, err)
	}
	return path
}

const trustedFixture = `
document:
  rules:
    - path_pattern: "api/**"
      required_doc_pattern: "docs/api/**"
review:
  required: true
disable_project_settings: true
no_ci: true
ci:
  required: true
test:
  evidence:
    branch: trusted-evidence-branch
commands:
  test: trusted-test-cmd
  lint: trusted-lint-cmd
agent: codex
agents:
  - codex
  - codex
allow_repo_commands: false
`

const trustedFixtureAllowRepoCommands = `
document:
  rules:
    - path_pattern: "api/**"
      required_doc_pattern: "docs/api/**"
review:
  required: true
disable_project_settings: true
no_ci: true
ci:
  required: true
test:
  evidence:
    branch: trusted-evidence-branch
commands:
  test: trusted-test-cmd
  lint: trusted-lint-cmd
agent: codex
agents:
  - codex
allow_repo_commands: true
`

const pushedFixture = `
document:
  rules:
    - path_pattern: "pushed/**"
      required_doc_pattern: "pushed-docs/**"
review:
  required: false
disable_project_settings: false
no_ci: false
ci:
  required: false
test:
  evidence:
    branch: pushed-evidence-branch
commands:
  test: pushed-test-cmd
  lint: pushed-lint-cmd
agent: codex
agents:
  - codex
allow_repo_commands: true
`

// Rule (a): Document, Review, DisableProjectSettings, NoCI, CI, and
// Test.Evidence.Branch are always taken from the trusted copy, regardless of
// what the pushed copy says.
func TestLoadEffectiveConfig_RuleA_AlwaysReadFromTrusted(t *testing.T) {
	dir := t.TempDir()
	trustedPath := writeConfigFile(t, dir, "trusted.yaml", trustedFixture)
	pushedPath := writeConfigFile(t, dir, "pushed.yaml", pushedFixture)

	cfg, err := LoadEffectiveConfig(trustedPath, pushedPath)
	if err != nil {
		t.Fatalf("LoadEffectiveConfig returned unexpected error: %v", err)
	}

	if len(cfg.Document.Rules) != 1 || cfg.Document.Rules[0].PathPattern != "api/**" {
		t.Errorf("Document = %+v, want trusted copy's rules (api/**), pushed copy must be ignored", cfg.Document)
	}
	if !cfg.Review.Required {
		t.Errorf("Review.Required = false, want true (trusted copy value), pushed copy sets false")
	}
	if !cfg.DisableProjectSettings {
		t.Errorf("DisableProjectSettings = false, want true (trusted copy value)")
	}
	if !cfg.NoCI {
		t.Errorf("NoCI = false, want true (trusted copy value)")
	}
	if !cfg.CI.Required {
		t.Errorf("CI.Required = false, want true (trusted copy value)")
	}
	if cfg.Test.Evidence.Branch != "trusted-evidence-branch" {
		t.Errorf("Test.Evidence.Branch = %q, want %q (trusted copy value)", cfg.Test.Evidence.Branch, "trusted-evidence-branch")
	}
}

// Rule (b), default case: Commands/Agent/Agents come from the trusted copy
// when the trusted copy does not set allow_repo_commands.
func TestLoadEffectiveConfig_RuleB_CommandsTrustedByDefault(t *testing.T) {
	dir := t.TempDir()
	trustedPath := writeConfigFile(t, dir, "trusted.yaml", trustedFixture)
	pushedPath := writeConfigFile(t, dir, "pushed.yaml", pushedFixture)

	cfg, err := LoadEffectiveConfig(trustedPath, pushedPath)
	if err != nil {
		t.Fatalf("LoadEffectiveConfig returned unexpected error: %v", err)
	}

	if cfg.Commands.Test != "trusted-test-cmd" {
		t.Errorf("Commands.Test = %q, want trusted copy's value %q (pushed copy must be ignored)", cfg.Commands.Test, "trusted-test-cmd")
	}
	if cfg.Commands.Lint != "trusted-lint-cmd" {
		t.Errorf("Commands.Lint = %q, want trusted copy's value %q", cfg.Commands.Lint, "trusted-lint-cmd")
	}
	if cfg.Agent != "codex" {
		t.Errorf("Agent = %q, want trusted copy's value %q", cfg.Agent, "codex")
	}
	if len(cfg.Agents) != 2 || cfg.Agents[0] != "codex" {
		t.Errorf("Agents = %v, want trusted copy's value", cfg.Agents)
	}
}

// Rule (b), opt-in case: when the trusted copy itself sets
// allow_repo_commands: true, the pushed copy's Commands/Agent/Agents are
// honored instead.
func TestLoadEffectiveConfig_RuleB_PushedHonoredWhenAllowRepoCommands(t *testing.T) {
	dir := t.TempDir()
	trustedPath := writeConfigFile(t, dir, "trusted.yaml", trustedFixtureAllowRepoCommands)
	pushedPath := writeConfigFile(t, dir, "pushed.yaml", pushedFixture)

	cfg, err := LoadEffectiveConfig(trustedPath, pushedPath)
	if err != nil {
		t.Fatalf("LoadEffectiveConfig returned unexpected error: %v", err)
	}

	if cfg.Commands.Test != "pushed-test-cmd" {
		t.Errorf("Commands.Test = %q, want pushed copy's value %q (allow_repo_commands: true opts in)", cfg.Commands.Test, "pushed-test-cmd")
	}
	if cfg.Commands.Lint != "pushed-lint-cmd" {
		t.Errorf("Commands.Lint = %q, want pushed copy's value %q", cfg.Commands.Lint, "pushed-lint-cmd")
	}
	if cfg.Agent != "codex" {
		t.Errorf("Agent = %q, want pushed copy's value %q", cfg.Agent, "codex")
	}
	if len(cfg.Agents) != 1 || cfg.Agents[0] != "codex" {
		t.Errorf("Agents = %v, want pushed copy's value", cfg.Agents)
	}

	// Rule (a) fields must still come from the trusted copy even though
	// allow_repo_commands opted the executable fields into the pushed copy.
	if !cfg.Review.Required {
		t.Errorf("Review.Required = false, want true (trusted copy value, unaffected by allow_repo_commands)")
	}
}

// Rule (c): with no trusted copy at all, the executable fields must resolve
// to empty/zero-value - never silently fall back to the pushed copy.
func TestLoadEffectiveConfig_RuleC_NoTrustedCopyZeroesExecutableFields(t *testing.T) {
	dir := t.TempDir()
	pushedPath := writeConfigFile(t, dir, "pushed.yaml", pushedFixture)

	cfg, err := LoadEffectiveConfig("", pushedPath)
	if err != nil {
		t.Fatalf("LoadEffectiveConfig returned unexpected error: %v", err)
	}

	if len(cfg.Commands.Test) != 0 || len(cfg.Commands.Lint) != 0 {
		t.Errorf("Commands = %+v, want zero-value (no trusted copy present)", cfg.Commands)
	}
	if cfg.Agent != "" {
		t.Errorf("Agent = %q, want empty (no trusted copy present)", cfg.Agent)
	}
	if len(cfg.Agents) != 0 {
		t.Errorf("Agents = %v, want empty (no trusted copy present)", cfg.Agents)
	}

	// Also verify with a trusted path pointing at a nonexistent file, which
	// must be treated the same as "no trusted copy" (not a read error).
	cfg2, err := LoadEffectiveConfig(filepath.Join(dir, "does-not-exist.yaml"), pushedPath)
	if err != nil {
		t.Fatalf("LoadEffectiveConfig with nonexistent trusted path returned unexpected error: %v", err)
	}
	if len(cfg2.Commands.Test) != 0 || cfg2.Agent != "" || len(cfg2.Agents) != 0 {
		t.Errorf("executable fields = %+v/%q/%v, want zero-value for nonexistent trusted file", cfg2.Commands, cfg2.Agent, cfg2.Agents)
	}
}

// Rule (d): if a trusted copy exists but cannot be read - permissions error
// or parse error - the loader must return a non-nil error. It must never
// proceed with defaults or a partial config.
func TestLoadEffectiveConfig_RuleD_UnreadableTrustedConfigFailsClosed(t *testing.T) {
	t.Run("permission denied", func(t *testing.T) {
		if os.Geteuid() == 0 {
			t.Skip("running as root: permission bits do not block reads")
		}
		dir := t.TempDir()
		trustedPath := writeConfigFile(t, dir, "trusted.yaml", trustedFixture)
		pushedPath := writeConfigFile(t, dir, "pushed.yaml", pushedFixture)

		if err := os.Chmod(trustedPath, 0o000); err != nil {
			t.Fatalf("chmod trusted config: %v", err)
		}
		t.Cleanup(func() { _ = os.Chmod(trustedPath, 0o644) })

		cfg, err := LoadEffectiveConfig(trustedPath, pushedPath)
		if err == nil {
			t.Fatalf("LoadEffectiveConfig returned nil error for an unreadable trusted config; got cfg=%+v", cfg)
		}
	})

	t.Run("parse error", func(t *testing.T) {
		dir := t.TempDir()
		trustedPath := writeConfigFile(t, dir, "trusted.yaml", "not: valid: yaml: [unterminated")
		pushedPath := writeConfigFile(t, dir, "pushed.yaml", pushedFixture)

		cfg, err := LoadEffectiveConfig(trustedPath, pushedPath)
		if err == nil {
			t.Fatalf("LoadEffectiveConfig returned nil error for a malformed trusted config; got cfg=%+v", cfg)
		}
	})
}
