package cursor_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/douglasjarquin/made/internal/managed"
)

// TestReviewerOutputShape_RoundTripsThroughRealExternalReviewParser proves the
// exact JSON shape the generated reviewer (internal/cursor.ReviewerMarkdown)
// instructs a Cursor reviewer to emit actually parses through #39's real
// managed.ParseExternalReviewResult, not a reimplementation of it.
func TestReviewerOutputShape_RoundTripsThroughRealExternalReviewParser(t *testing.T) {
	identity := managed.ExternalReviewIdentity{
		BaseSHA:      strings.Repeat("a", 40),
		InputSHA:     strings.Repeat("b", 40),
		PolicyHash:   "sha256:" + strings.Repeat("c", 64),
		ContractHash: "sha256:" + strings.Repeat("d", 64),
	}
	result := managed.ExternalReviewResult{
		SchemaVersion:         managed.ExternalReviewSchemaVersion,
		ReviewContractVersion: managed.ReviewContractVersion,
		Executor:              "cursor",
		Reviewer:              "made-reviewer",
		RequestedModel:        "claude-opus-5[effort=high,context=300k]",
		ActualModel:           "",
		BaseSHA:               identity.BaseSHA,
		InputSHA:              identity.InputSHA,
		PolicyHash:            identity.PolicyHash,
		ReviewContractHash:    identity.ContractHash,
		Findings:              nil,
	}

	path := writeJSONFile(t, result)
	if _, err := managed.ParseExternalReviewResult(path, identity); err != nil {
		t.Fatalf("expected the reviewer's documented output shape to parse cleanly, got %v", err)
	}
}

func TestReviewerOutputShape_MalformedFreeFormOutputIsRejected(t *testing.T) {
	identity := managed.ExternalReviewIdentity{
		BaseSHA:      strings.Repeat("a", 40),
		InputSHA:     strings.Repeat("b", 40),
		PolicyHash:   "sha256:" + strings.Repeat("c", 64),
		ContractHash: "sha256:" + strings.Repeat("d", 64),
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "result.json")
	prose := "I reviewed the change and it looks fine, no findings."
	if err := os.WriteFile(path, []byte(prose), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := managed.ParseExternalReviewResult(path, identity); err == nil {
		t.Fatal("expected free-form prose to be rejected by the real external review parser")
	}
}

func writeJSONFile(t *testing.T, v any) string {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "result.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
