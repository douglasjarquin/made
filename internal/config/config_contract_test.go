package config

import "testing"

func TestLoadEffectiveConfig_RejectsUnknownSemanticSwitch(t *testing.T) {
	dir := t.TempDir()
	trustedPath := writeConfigFile(t, dir, "trusted.yaml", "review:\n  required: true\nunknown_switch: true\n")

	if _, err := LoadEffectiveConfig(trustedPath, ""); err == nil {
		t.Fatal("expected unknown semantic configuration switch to fail closed")
	}
}
