package receipt_test

import (
	"testing"

	"github.com/douglasjarquin/made/internal/receipt"
)

func baseLaneFingerprintInputs() receipt.LaneFingerprintInputs {
	return receipt.LaneFingerprintInputs{
		RepoIdentity: "https://github.com/douglasjarquin/made.git",
		BaseSHA:      "aaa111",
		CandidateSHA: "bbb222",
		ConfigHash:   "sha256:configabc",
		LaneName:     "go",
		MatchedPaths: []string{"b.go", "a.go"},
		Command:      []string{"sh", "-c", "go build ./..."},
		MadeVersion:  "dev",
	}
}

func TestBuildLaneFingerprint_PopulatesFixedFields(t *testing.T) {
	fp := receipt.BuildLaneFingerprint(baseLaneFingerprintInputs())
	if fp.SchemaVersion != receipt.FingerprintSchemaVersion {
		t.Errorf("SchemaVersion = %d, want %d", fp.SchemaVersion, receipt.FingerprintSchemaVersion)
	}
	if fp.ValidationLevel != "full" {
		t.Errorf("ValidationLevel = %q, want %q", fp.ValidationLevel, "full")
	}
	if fp.WorkingDirectory != "." {
		t.Errorf("WorkingDirectory = %q, want %q", fp.WorkingDirectory, ".")
	}
	if fp.Lane != "go" || fp.BaseSHA != "aaa111" || fp.CandidateSHA != "bbb222" || fp.ConfigHash != "sha256:configabc" {
		t.Fatalf("unexpected identity fields: %+v", fp)
	}
	if fp.InputSetHash == "" || fp.ToolchainHash == "" {
		t.Fatalf("expected computed hashes to be populated, got %+v", fp)
	}
}

func TestBuildLaneFingerprint_MatchedPathOrderDoesNotAffectHash(t *testing.T) {
	forward := baseLaneFingerprintInputs()
	forward.MatchedPaths = []string{"a.go", "b.go"}
	reversed := baseLaneFingerprintInputs()
	reversed.MatchedPaths = []string{"b.go", "a.go"}

	if receipt.BuildLaneFingerprint(forward).Hash() != receipt.BuildLaneFingerprint(reversed).Hash() {
		t.Fatal("expected matched-path order to be normalized before hashing")
	}
}

func TestToolchainFingerprint_IsDeterministicWithinAProcess(t *testing.T) {
	first := receipt.ToolchainFingerprint()
	second := receipt.ToolchainFingerprint()
	if first != second {
		t.Fatal("expected ToolchainFingerprint to be stable within one process")
	}
}
