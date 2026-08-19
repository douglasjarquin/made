package managed_test

import (
	"testing"

	"github.com/douglasjarquin/made/internal/managed"
)

func TestFingerprint_DeterministicForSameInput(t *testing.T) {
	in := managed.FingerprintInput{
		Stage:       "review",
		Kind:        "ask-user",
		Code:        "review.arch",
		Class:       "project-judgment",
		Symbol:      "MyFunc",
		Paths:       []string{"internal/foo.go", "internal/bar.go"},
		Description: "Missing ADR for new dependency",
	}
	fp1 := managed.Fingerprint(in)
	fp2 := managed.Fingerprint(in)
	if fp1 != fp2 {
		t.Errorf("fingerprint not deterministic: %q vs %q", fp1, fp2)
	}
}

func TestFingerprint_PathOrderingDoesNotMatter(t *testing.T) {
	base := managed.FingerprintInput{
		Stage: "review", Kind: "ask-user", Code: "rc",
		Paths: []string{"a.go", "b.go"},
	}
	reversed := managed.FingerprintInput{
		Stage: "review", Kind: "ask-user", Code: "rc",
		Paths: []string{"b.go", "a.go"},
	}
	if managed.Fingerprint(base) != managed.Fingerprint(reversed) {
		t.Error("path ordering should not affect fingerprint")
	}
}

func TestFingerprint_DuplicatePathsDoNotMatter(t *testing.T) {
	base := managed.FingerprintInput{
		Stage: "review", Kind: "ask-user", Code: "rc",
		Paths: []string{"a.go"},
	}
	dup := managed.FingerprintInput{
		Stage: "review", Kind: "ask-user", Code: "rc",
		Paths: []string{"a.go", "a.go"},
	}
	if managed.Fingerprint(base) != managed.Fingerprint(dup) {
		t.Error("duplicate paths should not affect fingerprint")
	}
}

func TestFingerprint_AbsoluteWorkspacePrefixNotLeaked(t *testing.T) {
	withPrefix := managed.FingerprintInput{
		Stage:           "review",
		Kind:            "ask-user",
		Code:            "rc",
		Paths:           []string{"/workspace/repo/a.go"},
		WorkspacePrefix: "/workspace/repo",
	}
	withoutPrefix := managed.FingerprintInput{
		Stage: "review",
		Kind:  "ask-user",
		Code:  "rc",
		Paths: []string{"a.go"},
	}
	if managed.Fingerprint(withPrefix) != managed.Fingerprint(withoutPrefix) {
		t.Error("absolute workspace prefix should be stripped before fingerprinting")
	}
}

func TestFingerprint_DifferentCodeProducesDifferentFingerprint(t *testing.T) {
	a := managed.FingerprintInput{Stage: "review", Kind: "ask-user", Code: "code-a"}
	b := managed.FingerprintInput{Stage: "review", Kind: "ask-user", Code: "code-b"}
	if managed.Fingerprint(a) == managed.Fingerprint(b) {
		t.Error("different code should produce different fingerprint")
	}
}

func TestFingerprint_DifferentSymbolProducesDifferentFingerprint(t *testing.T) {
	a := managed.FingerprintInput{Stage: "review", Kind: "ask-user", Code: "c", Symbol: "FuncA"}
	b := managed.FingerprintInput{Stage: "review", Kind: "ask-user", Code: "c", Symbol: "FuncB"}
	if managed.Fingerprint(a) == managed.Fingerprint(b) {
		t.Error("different symbol should produce different fingerprint")
	}
}

func TestFingerprint_DifferentStageProducesDifferentFingerprint(t *testing.T) {
	a := managed.FingerprintInput{Stage: "review", Kind: "ask-user", Code: "c"}
	b := managed.FingerprintInput{Stage: "document", Kind: "ask-user", Code: "c"}
	if managed.Fingerprint(a) == managed.Fingerprint(b) {
		t.Error("different stage should produce different fingerprint")
	}
}

func TestFingerprint_StartsWithSHA256Prefix(t *testing.T) {
	fp := managed.Fingerprint(managed.FingerprintInput{
		Stage: "review", Kind: "ask-user", Code: "c",
	})
	if len(fp) < 7 || fp[:7] != "sha256:" {
		t.Errorf("fingerprint %q should start with sha256:", fp)
	}
	if len(fp) != 7+64 {
		t.Errorf("fingerprint %q should be sha256: + 64 hex chars, got len %d", fp, len(fp))
	}
}

func TestFingerprint_FallbackNormalizationIsDeterministic(t *testing.T) {
	// Same description with extra whitespace should produce same fingerprint.
	a := managed.FingerprintInput{Stage: "review", Kind: "ask-user", Description: "foo  bar"}
	b := managed.FingerprintInput{Stage: "review", Kind: "ask-user", Description: "foo bar"}
	if managed.Fingerprint(a) != managed.Fingerprint(b) {
		t.Error("whitespace normalization should produce same fingerprint")
	}
}
