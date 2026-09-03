package managed_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/douglasjarquin/made/internal/managed"
)

func writeExternalResult(t *testing.T, v any) string {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "review-result.json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func baseExternalResult(identity managed.ExternalReviewIdentity) map[string]any {
	return map[string]any{
		"schema_version":          managed.ExternalReviewSchemaVersion,
		"review_contract_version": managed.ReviewContractVersion,
		"executor":                "cursor",
		"reviewer":                "made-reviewer",
		"requested_model":         "claude-opus-5[effort=high]",
		"actual_model":            "",
		"base_sha":                identity.BaseSHA,
		"input_sha":               identity.InputSHA,
		"policy_hash":             identity.PolicyHash,
		"review_contract_hash":    identity.ContractHash,
		"findings":                []any{},
	}
}

func testIdentity() managed.ExternalReviewIdentity {
	return managed.ExternalReviewIdentity{
		BaseSHA:      strings.Repeat("1", 40),
		InputSHA:     strings.Repeat("2", 40),
		PolicyHash:   "sha256:" + strings.Repeat("a", 64),
		ContractHash: "sha256:" + strings.Repeat("b", 64),
	}
}

func TestExternalReview_ValidEmpty(t *testing.T) {
	identity := testIdentity()
	path := writeExternalResult(t, baseExternalResult(identity))
	result, err := managed.ParseExternalReviewResult(path, identity)
	if err != nil {
		t.Fatalf("expected valid empty result to parse, got: %v", err)
	}
	if len(result.Findings) != 0 {
		t.Errorf("expected no findings, got %d", len(result.Findings))
	}
}

func TestExternalReview_ValidFindingKinds(t *testing.T) {
	identity := testIdentity()
	payload := baseExternalResult(identity)
	payload["findings"] = []map[string]any{
		{"kind": "auto-fixable", "description": "needs gofmt", "code": "style.gofmt", "class": "style", "paths": []string{"main.go"}, "patch": "diff"},
		{"kind": "ask-user", "description": "judgment call", "code": "arch.dep", "class": "architecture", "paths": []string{"go.mod"}},
		{"kind": "blocking", "description": "sql injection", "code": "sec.sqli", "class": "security", "paths": []string{"db.go"}},
	}
	path := writeExternalResult(t, payload)
	result, err := managed.ParseExternalReviewResult(path, identity)
	if err != nil {
		t.Fatalf("expected valid findings to parse, got: %v", err)
	}
	if len(result.Findings) != 3 {
		t.Fatalf("expected 3 findings, got %d", len(result.Findings))
	}
}

func TestExternalReview_WrongBaseSHARejected(t *testing.T) {
	identity := testIdentity()
	payload := baseExternalResult(identity)
	payload["base_sha"] = strings.Repeat("9", 40)
	path := writeExternalResult(t, payload)
	if _, err := managed.ParseExternalReviewResult(path, identity); err == nil {
		t.Fatal("expected error for wrong base_sha")
	}
}

func TestExternalReview_WrongInputSHARejected(t *testing.T) {
	identity := testIdentity()
	payload := baseExternalResult(identity)
	payload["input_sha"] = strings.Repeat("9", 40)
	path := writeExternalResult(t, payload)
	if _, err := managed.ParseExternalReviewResult(path, identity); err == nil {
		t.Fatal("expected error for wrong input_sha")
	}
}

func TestExternalReview_WrongPolicyHashRejected(t *testing.T) {
	identity := testIdentity()
	payload := baseExternalResult(identity)
	payload["policy_hash"] = "sha256:" + strings.Repeat("9", 64)
	path := writeExternalResult(t, payload)
	if _, err := managed.ParseExternalReviewResult(path, identity); err == nil {
		t.Fatal("expected error for wrong policy_hash")
	}
}

func TestExternalReview_WrongContractHashRejected(t *testing.T) {
	identity := testIdentity()
	payload := baseExternalResult(identity)
	payload["review_contract_hash"] = "sha256:" + strings.Repeat("9", 64)
	path := writeExternalResult(t, payload)
	if _, err := managed.ParseExternalReviewResult(path, identity); err == nil {
		t.Fatal("expected error for wrong review_contract_hash")
	}
}

func TestExternalReview_UnsupportedSchemaVersionRejected(t *testing.T) {
	identity := testIdentity()
	payload := baseExternalResult(identity)
	payload["schema_version"] = 99
	path := writeExternalResult(t, payload)
	if _, err := managed.ParseExternalReviewResult(path, identity); err == nil {
		t.Fatal("expected error for unsupported schema_version")
	}
}

func TestExternalReview_UnsupportedContractVersionRejected(t *testing.T) {
	identity := testIdentity()
	payload := baseExternalResult(identity)
	payload["review_contract_version"] = 99
	path := writeExternalResult(t, payload)
	if _, err := managed.ParseExternalReviewResult(path, identity); err == nil {
		t.Fatal("expected error for unsupported review_contract_version")
	}
}

func TestExternalReview_UnknownFieldRejected(t *testing.T) {
	identity := testIdentity()
	payload := baseExternalResult(identity)
	payload["unexpected_field"] = "surprise"
	path := writeExternalResult(t, payload)
	if _, err := managed.ParseExternalReviewResult(path, identity); err == nil {
		t.Fatal("expected error for unknown field")
	}
}

func TestExternalReview_MissingFieldRejected(t *testing.T) {
	identity := testIdentity()
	payload := baseExternalResult(identity)
	delete(payload, "base_sha")
	path := writeExternalResult(t, payload)
	if _, err := managed.ParseExternalReviewResult(path, identity); err == nil {
		t.Fatal("expected error for missing base_sha")
	}
}

func TestExternalReview_OversizedFileRejected(t *testing.T) {
	identity := testIdentity()
	payload := baseExternalResult(identity)
	payload["reviewer"] = strings.Repeat("x", 3*1024*1024)
	path := writeExternalResult(t, payload)
	if _, err := managed.ParseExternalReviewResult(path, identity); err == nil {
		t.Fatal("expected error for oversized file")
	}
}

func TestExternalReview_OversizedStringRejected(t *testing.T) {
	identity := testIdentity()
	payload := baseExternalResult(identity)
	payload["reviewer"] = strings.Repeat("x", 4000)
	path := writeExternalResult(t, payload)
	if _, err := managed.ParseExternalReviewResult(path, identity); err == nil {
		t.Fatal("expected error for oversized reviewer string")
	}
}

func TestExternalReview_OversizedFindingCountRejected(t *testing.T) {
	identity := testIdentity()
	payload := baseExternalResult(identity)
	findings := make([]map[string]any, 0, 300)
	for i := 0; i < 300; i++ {
		findings = append(findings, map[string]any{
			"kind": "ask-user", "description": "d", "code": "code", "class": "class",
			"paths": []string{"main.go"}, "symbol": strconv.Itoa(i),
		})
	}
	payload["findings"] = findings
	path := writeExternalResult(t, payload)
	if _, err := managed.ParseExternalReviewResult(path, identity); err == nil {
		t.Fatal("expected error for oversized finding count")
	}
}

func TestExternalReview_UnsafeFindingPathRejected(t *testing.T) {
	identity := testIdentity()
	payload := baseExternalResult(identity)
	payload["findings"] = []map[string]any{
		{"kind": "ask-user", "description": "d", "code": "c", "class": "cl", "paths": []string{"/etc/passwd"}},
	}
	path := writeExternalResult(t, payload)
	if _, err := managed.ParseExternalReviewResult(path, identity); err == nil {
		t.Fatal("expected error for unsafe absolute finding path")
	}
}

func TestExternalReview_DuplicateFingerprintRejected(t *testing.T) {
	identity := testIdentity()
	payload := baseExternalResult(identity)
	f := map[string]any{"kind": "ask-user", "description": "d", "code": "c", "class": "cl", "paths": []string{"main.go"}}
	payload["findings"] = []map[string]any{f, f}
	path := writeExternalResult(t, payload)
	if _, err := managed.ParseExternalReviewResult(path, identity); err == nil {
		t.Fatal("expected error for duplicate fingerprint")
	}
}

func TestExternalReview_ConflictingFingerprintRejected(t *testing.T) {
	identity := testIdentity()
	payload := baseExternalResult(identity)
	payload["findings"] = []map[string]any{
		{"kind": "ask-user", "description": "d1", "code": "c", "class": "cl", "paths": []string{"main.go"}},
		{"kind": "blocking", "description": "d2", "code": "c", "class": "cl", "paths": []string{"main.go"}},
	}
	path := writeExternalResult(t, payload)
	if _, err := managed.ParseExternalReviewResult(path, identity); err == nil {
		t.Fatal("expected error for conflicting fingerprint (same identity, different kind)")
	}
}

func TestExternalReview_ActualModelAbsentAcceptedAsProvenanceOnly(t *testing.T) {
	identity := testIdentity()
	payload := baseExternalResult(identity)
	payload["actual_model"] = ""
	path := writeExternalResult(t, payload)
	if _, err := managed.ParseExternalReviewResult(path, identity); err != nil {
		t.Fatalf("expected empty actual_model to be accepted, got: %v", err)
	}
}

func TestExternalReview_ActualModelSubstitutedAcceptedAsProvenanceOnly(t *testing.T) {
	identity := testIdentity()
	payload := baseExternalResult(identity)
	payload["actual_model"] = "a-different-model-than-requested"
	path := writeExternalResult(t, payload)
	if _, err := managed.ParseExternalReviewResult(path, identity); err != nil {
		t.Fatalf("expected substituted actual_model to be accepted, got: %v", err)
	}
}

func testIdentityWithGuides() managed.ExternalReviewIdentity {
	identity := testIdentity()
	identity.Guides = []managed.GuideBinding{
		{Path: ".made/features/README.md", ContentHash: "sha256:" + strings.Repeat("f", 64), Bytes: 10},
	}
	return identity
}

func TestExternalReview_GuidesConsulted_ValidEchoAccepted(t *testing.T) {
	identity := testIdentityWithGuides()
	payload := baseExternalResult(identity)
	payload["guides_consulted"] = []map[string]any{
		{"path": ".made/features/README.md", "content_hash": identity.Guides[0].ContentHash},
	}
	path := writeExternalResult(t, payload)
	result, err := managed.ParseExternalReviewResult(path, identity)
	if err != nil {
		t.Fatalf("expected valid guides_consulted to parse, got: %v", err)
	}
	if len(result.GuidesConsulted) != 1 || result.GuidesConsulted[0].Path != ".made/features/README.md" {
		t.Fatalf("expected guides_consulted to round-trip, got %+v", result.GuidesConsulted)
	}
}

func TestExternalReview_GuidesConsulted_NotRequiredWhenGuidesConfigured(t *testing.T) {
	identity := testIdentityWithGuides()
	payload := baseExternalResult(identity)
	path := writeExternalResult(t, payload)
	if _, err := managed.ParseExternalReviewResult(path, identity); err != nil {
		t.Fatalf("expected omitted guides_consulted to be accepted, got: %v", err)
	}
}

func TestExternalReview_GuidesConsulted_InventedPathRejected(t *testing.T) {
	identity := testIdentityWithGuides()
	payload := baseExternalResult(identity)
	payload["guides_consulted"] = []map[string]any{
		{"path": "docs/never-configured.md", "content_hash": "sha256:" + strings.Repeat("9", 64)},
	}
	path := writeExternalResult(t, payload)
	if _, err := managed.ParseExternalReviewResult(path, identity); err == nil {
		t.Fatal("expected error for a guides_consulted entry referencing an unconfigured guide")
	}
}

func TestExternalReview_GuidesConsulted_StaleContentHashRejected(t *testing.T) {
	identity := testIdentityWithGuides()
	payload := baseExternalResult(identity)
	payload["guides_consulted"] = []map[string]any{
		{"path": ".made/features/README.md", "content_hash": "sha256:" + strings.Repeat("9", 64)},
	}
	path := writeExternalResult(t, payload)
	if _, err := managed.ParseExternalReviewResult(path, identity); err == nil {
		t.Fatal("expected error for a stale guides_consulted content_hash")
	}
}

func TestExternalReview_GuidesConsulted_DuplicatePathRejected(t *testing.T) {
	identity := testIdentityWithGuides()
	payload := baseExternalResult(identity)
	entry := map[string]any{"path": ".made/features/README.md", "content_hash": identity.Guides[0].ContentHash}
	payload["guides_consulted"] = []map[string]any{entry, entry}
	path := writeExternalResult(t, payload)
	if _, err := managed.ParseExternalReviewResult(path, identity); err == nil {
		t.Fatal("expected error for duplicate guides_consulted path")
	}
}

func TestExternalReview_GuidesConsulted_MissingContentHashRejected(t *testing.T) {
	identity := testIdentityWithGuides()
	payload := baseExternalResult(identity)
	payload["guides_consulted"] = []map[string]any{
		{"path": ".made/features/README.md", "content_hash": ""},
	}
	path := writeExternalResult(t, payload)
	if _, err := managed.ParseExternalReviewResult(path, identity); err == nil {
		t.Fatal("expected error for missing guides_consulted content_hash")
	}
}

func TestExternalReview_GuidesConsulted_OversizedCountRejected(t *testing.T) {
	identity := testIdentityWithGuides()
	payload := baseExternalResult(identity)
	consulted := make([]map[string]any, 0, 25)
	for i := 0; i < 25; i++ {
		consulted = append(consulted, map[string]any{
			"path": ".made/features/README.md", "content_hash": identity.Guides[0].ContentHash,
		})
	}
	payload["guides_consulted"] = consulted
	path := writeExternalResult(t, payload)
	if _, err := managed.ParseExternalReviewResult(path, identity); err == nil {
		t.Fatal("expected error for oversized guides_consulted count")
	}
}

func TestExternalReview_ActualModelEqualToRequestedAccepted(t *testing.T) {
	identity := testIdentity()
	payload := baseExternalResult(identity)
	payload["actual_model"] = payload["requested_model"]
	path := writeExternalResult(t, payload)
	if _, err := managed.ParseExternalReviewResult(path, identity); err != nil {
		t.Fatalf("expected actual_model equal to requested_model to be accepted, got: %v", err)
	}
}
