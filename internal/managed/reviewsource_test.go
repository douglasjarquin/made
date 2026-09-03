package managed_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/douglasjarquin/made/internal/agent"
	"github.com/douglasjarquin/made/internal/managed"
)

func TestReviewSource_ExternalEndToEndPasses(t *testing.T) {
	workspace, baseSHA, inputSHA := makeTestRepo(t)
	configPath, policyHash := makeTrustedConfig(t, `version: 1
review:
  required: true
`)
	evidenceDir := makeEvidenceDir(t)

	contractHash, err := managed.BuildReviewContract(baseSHA, inputSHA, policyHash).Hash()
	if err != nil {
		t.Fatal(err)
	}
	resultPath := filepath.Join(t.TempDir(), "review-result.json")
	result := map[string]any{
		"schema_version":          managed.ExternalReviewSchemaVersion,
		"review_contract_version": managed.ReviewContractVersion,
		"executor":                "cursor",
		"reviewer":                "cursor-cloud",
		"requested_model":         "claude-opus-5",
		"actual_model":            nil,
		"base_sha":                baseSHA,
		"input_sha":               inputSHA,
		"policy_hash":             policyHash,
		"review_contract_hash":    contractHash,
		"findings":                []any{},
	}
	data, _ := json.Marshal(result)
	if err := os.WriteFile(resultPath, data, 0o644); err != nil {
		t.Fatal(err)
	}

	opts := &managed.Options{
		RunID:         "G-ext",
		MissionID:     "M-ext",
		Workspace:     workspace,
		BaseSHA:       baseSHA,
		InputSHA:      inputSHA,
		TrustedConfig: configPath,
		PolicyHash:    policyHash,
		EvidenceDir:   evidenceDir,
		ReviewSource:  "external",
		ReviewResult:  resultPath,
	}
	res := runManaged(t, context.Background(), opts)
	if res.exitCode != 0 {
		t.Fatalf("expected exit 0, got %d (stderr: %s)", res.exitCode, res.stderr)
	}
	if got := terminalOutcome(t, res.events); got != "passed" {
		t.Errorf("expected outcome passed, got %q", got)
	}
	if got := stageOutcome(t, res.events, "review"); got != "passed" {
		t.Errorf("review outcome = %q, want passed", got)
	}
}

func TestReviewSource_ExternalMissingResultFileFails(t *testing.T) {
	workspace, baseSHA, inputSHA := makeTestRepo(t)
	configPath, policyHash := makeTrustedConfig(t, `version: 1
review:
  required: true
`)
	evidenceDir := makeEvidenceDir(t)

	opts := &managed.Options{
		RunID:         "G-ext-missing",
		MissionID:     "M-ext-missing",
		Workspace:     workspace,
		BaseSHA:       baseSHA,
		InputSHA:      inputSHA,
		TrustedConfig: configPath,
		PolicyHash:    policyHash,
		EvidenceDir:   evidenceDir,
		ReviewSource:  "external",
		ReviewResult:  filepath.Join(t.TempDir(), "does-not-exist.json"),
	}
	res := runManaged(t, context.Background(), opts)
	if res.exitCode == 0 {
		t.Fatal("expected non-zero exit for missing external review result")
	}
	if got := terminalOutcome(t, res.events); got != "infrastructure_error" {
		t.Errorf("expected outcome infrastructure_error, got %q", got)
	}
}

func TestReviewSource_RequiredReviewMissingInternalAgentRejectedAtConfigLoad(t *testing.T) {
	_, baseSHA, inputSHA := makeTestRepo(t)
	configPath, policyHash := makeTrustedConfig(t, `version: 1
review:
  required: true
`)
	evidenceDir := makeEvidenceDir(t)
	workspace, _, _ := makeTestRepo(t)

	opts := &managed.Options{
		RunID:         "G-noagent",
		MissionID:     "M-noagent",
		Workspace:     workspace,
		BaseSHA:       baseSHA,
		InputSHA:      inputSHA,
		TrustedConfig: configPath,
		PolicyHash:    policyHash,
		EvidenceDir:   evidenceDir,
	}
	res := runManaged(t, context.Background(), opts)
	if res.exitCode != 1 {
		t.Fatalf("expected exit 1 (infrastructure_error), got %d (stderr: %s)", res.exitCode, res.stderr)
	}
}

func TestReviewSource_InternalDefaultWhenFlagOmitted(t *testing.T) {
	opts := e2eOptions(t, "G-default-source", "M-default-source", agent.Findings{Findings: []agent.Finding{}})
	res := runManaged(t, context.Background(), opts)
	if res.exitCode != 0 {
		t.Fatalf("expected exit 0, got %d (stderr: %s)", res.exitCode, res.stderr)
	}
	if got := stageOutcome(t, res.events, "review"); got != "passed" {
		t.Errorf("review outcome = %q, want passed", got)
	}
}
