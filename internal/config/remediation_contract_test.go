package config

import "testing"

func TestLoadConfig_RejectsUnknownMadeYMLFields(t *testing.T) {
	path := writeConfigFile(t, t.TempDir(), ".made.yml", "version: 1\nunknown_field: true\n")

	if _, _, err := loadConfigFile(path); err == nil {
		t.Fatal("loadConfigFile accepted an unknown .made.yml field")
	}
}

func TestLoadConfig_RejectsUnknownFieldsInTrustedMadeYMLCopy(t *testing.T) {
	path := writeConfigFile(t, t.TempDir(), "trusted-copy.made.yml", "version: 1\nunknown_field: true\n")

	if _, _, err := loadConfigFile(path); err == nil {
		t.Fatal("loadConfigFile accepted an unknown field in a trusted .made.yml copy")
	}
}

func TestLoadConfig_RejectsZeroValueMadeYML(t *testing.T) {
	path := writeConfigFile(t, t.TempDir(), ".made.yml", "")

	if _, _, err := loadConfigFile(path); err == nil {
		t.Fatal("loadConfigFile accepted a zero-value .made.yml configuration")
	}
}

func TestLoadConfig_RejectsVersionOnlyMadeYML(t *testing.T) {
	path := writeConfigFile(t, t.TempDir(), ".made.yml", "version: 1\n")

	if _, _, err := loadConfigFile(path); err == nil {
		t.Fatal("loadConfigFile accepted a version-only .made.yml configuration")
	}
}

func TestConfig_DisabledStagesAreSkipped(t *testing.T) {
	path := writeConfigFile(t, t.TempDir(), ".made.yml", "version: 1\nstages:\n  review:\n    enabled: false\n")
	cfg, _, err := loadConfigFile(path)
	if err != nil {
		t.Fatalf("loadConfigFile: %v", err)
	}
	if got := cfg.StageResult("review"); got != "skipped" {
		t.Fatalf("disabled stage result = %q, want skipped", got)
	}
}

func TestConfig_NoCIIsRepresentedAsSkipped(t *testing.T) {
	if got := (Config{NoCI: true}).StageResult("ci"); got != "skipped" {
		t.Fatalf("NoCI stage result = %q, want skipped", got)
	}
}

func TestLoadConfig_RejectsUnknownStageKeys(t *testing.T) {
	path := writeConfigFile(t, t.TempDir(), ".made.yml", "version: 1\nstages:\n  reviw:\n    enabled: false\n")

	if _, _, err := loadConfigFile(path); err == nil {
		t.Fatal("loadConfigFile accepted an unknown stage key")
	}
}

func TestConfig_RequiredSettingsControlDisabledReviewAndCI(t *testing.T) {
	disabled := false
	cfg := Config{
		Stages: map[string]Stage{"review": {Enabled: &disabled}, "ci": {Enabled: &disabled}},
	}
	if cfg.StageRequired("review") || cfg.StageRequired("ci") {
		t.Fatal("review or CI was required without the trusted required setting")
	}
	cfg.Review.Required = true
	cfg.CI.Required = true
	if !cfg.StageRequired("review") || !cfg.StageRequired("ci") {
		t.Fatal("trusted required settings did not make disabled review and CI stages required")
	}
	cfg.NoCI = true
	if cfg.StageRequired("ci") {
		t.Fatal("NoCI did not disable the CI requirement")
	}
}
