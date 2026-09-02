package receipt_test

import (
	"testing"

	"github.com/douglasjarquin/made/internal/receipt"
)

func baseFingerprint() receipt.Fingerprint {
	return receipt.Fingerprint{
		SchemaVersion:    1,
		Lane:             "go",
		ValidationLevel:  "full",
		RepoIdentity:     "https://github.com/douglasjarquin/made.git",
		BaseSHA:          "aaa111",
		CandidateSHA:     "bbb222",
		ConfigHash:       "sha256:configabc",
		Command:          []string{"sh", "-c", "go build ./..."},
		WorkingDirectory: ".",
		InputSetHash:     "sha256:inputsabc",
		ToolchainHash:    "sha256:toolchainabc",
		OS:               "linux",
		Arch:             "amd64",
		MadeVersion:      "dev",
		ProtocolVersion:  1,
	}
}

func TestFingerprint_HashIsDeterministic(t *testing.T) {
	f := baseFingerprint()
	if f.Hash() != f.Hash() {
		t.Fatal("expected Hash() to be deterministic for identical fields")
	}
}

func TestFingerprint_HashChangesWithEachField(t *testing.T) {
	base := baseFingerprint()
	baseHash := base.Hash()

	mutations := []func(*receipt.Fingerprint){
		func(f *receipt.Fingerprint) { f.Lane = "docs" },
		func(f *receipt.Fingerprint) { f.ValidationLevel = "quick" },
		func(f *receipt.Fingerprint) { f.RepoIdentity = "https://github.com/other/repo.git" },
		func(f *receipt.Fingerprint) { f.BaseSHA = "different" },
		func(f *receipt.Fingerprint) { f.CandidateSHA = "different" },
		func(f *receipt.Fingerprint) { f.ConfigHash = "sha256:different" },
		func(f *receipt.Fingerprint) { f.Command = []string{"sh", "-c", "go test ./..."} },
		func(f *receipt.Fingerprint) { f.WorkingDirectory = "subdir" },
		func(f *receipt.Fingerprint) { f.InputSetHash = "sha256:different" },
		func(f *receipt.Fingerprint) { f.ToolchainHash = "sha256:different" },
		func(f *receipt.Fingerprint) { f.OS = "darwin" },
		func(f *receipt.Fingerprint) { f.Arch = "arm64" },
		func(f *receipt.Fingerprint) { f.MadeVersion = "1.2.3" },
		func(f *receipt.Fingerprint) { f.ProtocolVersion = 2 },
	}

	for i, mutate := range mutations {
		mutated := baseFingerprint()
		mutate(&mutated)
		if mutated.Hash() == baseHash {
			t.Fatalf("mutation %d: expected a different hash after changing one field, got the same hash", i)
		}
	}
}

func TestFingerprint_HashIsPrefixedForReadability(t *testing.T) {
	f := baseFingerprint()
	if len(f.Hash()) < len("sha256:")+10 {
		t.Fatalf("expected a sha256:-prefixed hash, got %q", f.Hash())
	}
}
