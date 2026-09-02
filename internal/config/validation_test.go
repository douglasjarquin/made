package config_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/douglasjarquin/made/internal/config"
)

func TestConfig_Validate_AcceptsWellFormedReceiptMaxAge(t *testing.T) {
	cfg := config.Config{Validation: config.Validation{ReceiptMaxAge: "72h"}}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected a well-formed receipt_max_age to validate, got %v", err)
	}
}

func TestConfig_Validate_RejectsMalformedReceiptMaxAge(t *testing.T) {
	cfg := config.Config{Validation: config.Validation{ReceiptMaxAge: "not-a-duration"}}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected an error for a malformed receipt_max_age")
	}
}

func TestConfig_Validate_RejectsNonPositiveReceiptMaxAge(t *testing.T) {
	cfg := config.Config{Validation: config.Validation{ReceiptMaxAge: "-1h"}}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected an error for a non-positive receipt_max_age")
	}
}

func TestValidation_EffectiveReceiptMaxAge_DefaultsWhenUnset(t *testing.T) {
	got, err := (config.Validation{}).EffectiveReceiptMaxAge()
	if err != nil {
		t.Fatalf("EffectiveReceiptMaxAge: %v", err)
	}
	if got != config.DefaultReceiptMaxAge {
		t.Fatalf("expected default %s, got %s", config.DefaultReceiptMaxAge, got)
	}
}

func TestValidation_EffectiveReceiptMaxAge_HonorsOverride(t *testing.T) {
	got, err := (config.Validation{ReceiptMaxAge: "1h"}).EffectiveReceiptMaxAge()
	if err != nil {
		t.Fatalf("EffectiveReceiptMaxAge: %v", err)
	}
	if got != time.Hour {
		t.Fatalf("expected 1h, got %s", got)
	}
}

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

func TestConfig_Validate_NoGuidesIsValid(t *testing.T) {
	if err := (config.Config{Review: config.Review{Guides: nil}}).Validate(); err != nil {
		t.Fatalf("expected no guides to be valid, got %v", err)
	}
}

func TestConfig_Validate_EmptyGuidesListIsValid(t *testing.T) {
	if err := (config.Config{Review: config.Review{Guides: []string{}}}).Validate(); err != nil {
		t.Fatalf("expected an empty guides list to be valid, got %v", err)
	}
}

func TestConfig_Validate_AcceptsMultipleValidGuides(t *testing.T) {
	cfg := config.Config{Review: config.Review{Guides: []string{
		".made/features/README.md",
		"docs/architecture.md",
		"docs/security-threat-model.md",
	}}}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected multiple valid guides to validate, got %v", err)
	}
}

func TestConfig_Validate_RejectsDuplicateNormalizedGuidePaths(t *testing.T) {
	cfg := config.Config{Review: config.Review{Guides: []string{
		"docs/architecture.md",
		"docs/./architecture.md",
	}}}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected an error for duplicate normalized guide paths")
	}
	if !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("expected error to mention duplicate guide paths, got %v", err)
	}
}

func TestConfig_Validate_RejectsAbsoluteGuidePath(t *testing.T) {
	cfg := config.Config{Review: config.Review{Guides: []string{"/etc/passwd"}}}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected an error for an absolute guide path")
	}
	if !strings.Contains(err.Error(), "absolute") {
		t.Fatalf("expected error to mention absolute path, got %v", err)
	}
}

func TestConfig_Validate_RejectsParentTraversalGuidePath(t *testing.T) {
	cfg := config.Config{Review: config.Review{Guides: []string{"../outside.md"}}}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected an error for a guide path that escapes the repository root")
	}
	if !strings.Contains(err.Error(), "escape") {
		t.Fatalf("expected error to mention repository-root escape, got %v", err)
	}
}

func TestConfig_Validate_RejectsEmptyGuidePath(t *testing.T) {
	cfg := config.Config{Review: config.Review{Guides: []string{""}}}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected an error for an empty guide path")
	}
}

func TestConfig_Validate_RejectsOversizedGuidePath(t *testing.T) {
	cfg := config.Config{Review: config.Review{Guides: []string{strings.Repeat("a", 600) + ".md"}}}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected an error for an oversized guide path")
	}
	if !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("expected error to mention the size bound, got %v", err)
	}
}

func TestConfig_Validate_RejectsTooManyGuides(t *testing.T) {
	guides := make([]string, 21)
	for i := range guides {
		guides[i] = fmt.Sprintf("docs/guide-%d.md", i)
	}
	cfg := config.Config{Review: config.Review{Guides: guides}}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected an error for too many configured guides")
	}
	if !strings.Contains(err.Error(), "maximum") {
		t.Fatalf("expected error to mention the maximum guide count, got %v", err)
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
