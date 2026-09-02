package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/douglasjarquin/made/internal/config"
)

func TestConfig_Validate_AcceptsWellFormedValidationLane(t *testing.T) {
	cfg := config.Config{
		Validation: config.Validation{
			Lanes: map[string]config.Lane{
				"go": {
					Paths:              []string{"**/*.go", "go.mod"},
					Quick:              []string{"go vet ./..."},
					Full:               []string{"go build ./...", "go test ./..."},
					RequiredBeforePush: true,
				},
			},
		},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected a well-formed lane to validate, got %v", err)
	}
}

func TestConfig_Validate_RejectsLaneWithNoPaths(t *testing.T) {
	cfg := config.Config{
		Validation: config.Validation{
			Lanes: map[string]config.Lane{
				"go": {Full: []string{"go build ./..."}},
			},
		},
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected an error for a lane with no path patterns")
	}
	if !strings.Contains(err.Error(), "go") {
		t.Fatalf("expected the error to name the offending lane, got %v", err)
	}
}

func TestConfig_Validate_RejectsMalformedGlobPattern(t *testing.T) {
	cfg := config.Config{
		Validation: config.Validation{
			Lanes: map[string]config.Lane{
				"go": {Paths: []string{"[unterminated"}},
			},
		},
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected an error for a malformed glob pattern")
	}
}

func TestConfig_Validate_RejectsEmptyLaneName(t *testing.T) {
	cfg := config.Config{
		Validation: config.Validation{
			Lanes: map[string]config.Lane{
				"": {Paths: []string{"**/*.go"}},
			},
		},
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected an error for an empty lane name")
	}
}

func TestConfig_Validate_NoLanesConfiguredIsValid(t *testing.T) {
	if err := (config.Config{}).Validate(); err != nil {
		t.Fatalf("expected no validation lanes to be valid (Phase 1 default-lane fallback), got %v", err)
	}
}

func TestLoadEffectiveConfig_RoundTripsValidationLanesFromTrusted(t *testing.T) {
	dir := t.TempDir()
	trustedPath := filepath.Join(dir, ".made.yml")
	contents := `
version: 1
validation:
  lanes:
    go:
      paths:
        - "**/*.go"
        - "go.mod"
      quick:
        - "go vet ./..."
      full:
        - "go build ./..."
        - "go test ./..."
      required_before_push: true
`
	if err := os.WriteFile(trustedPath, []byte(contents), 0o644); err != nil {
		t.Fatalf("write trusted config: %v", err)
	}

	cfg, err := config.LoadEffectiveConfig(trustedPath, "")
	if err != nil {
		t.Fatalf("LoadEffectiveConfig: %v", err)
	}

	lane, ok := cfg.Validation.Lanes["go"]
	if !ok {
		t.Fatalf("expected lane %q to round-trip, got lanes %+v", "go", cfg.Validation.Lanes)
	}
	if len(lane.Paths) != 2 || lane.Paths[0] != "**/*.go" {
		t.Fatalf("expected lane paths to round-trip, got %+v", lane.Paths)
	}
	if !lane.RequiredBeforePush {
		t.Fatal("expected required_before_push to round-trip as true")
	}
}
