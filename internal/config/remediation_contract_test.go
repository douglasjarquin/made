package config

import "testing"

func TestLoadConfig_RejectsUnknownMadeYMLFields(t *testing.T) {
	path := writeConfigFile(t, t.TempDir(), ".made.yml", "version: 1\nunknown_field: true\n")

	if _, _, err := loadConfigFile(path); err == nil {
		t.Fatal("loadConfigFile accepted an unknown .made.yml field")
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
