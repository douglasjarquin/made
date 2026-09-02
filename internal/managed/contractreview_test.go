package managed_test

import (
	"strings"
	"testing"

	"github.com/douglasjarquin/made/internal/managed"
)

func TestBuildReviewContract_NoGuidesOmitsGuideFields(t *testing.T) {
	contract := managed.BuildReviewContract(strings.Repeat("a", 40), strings.Repeat("b", 40), "sha256:"+strings.Repeat("c", 64), nil)
	if len(contract.Guides) != 0 {
		t.Fatalf("expected no guides, got %+v", contract.Guides)
	}
	if contract.GuideInstructions != "" {
		t.Fatalf("expected no guide instructions, got %q", contract.GuideInstructions)
	}
}

func TestBuildReviewContract_GuidesCarryPathHashAndBytes(t *testing.T) {
	guides := []managed.GuideBinding{
		{Path: ".made/features/README.md", ContentHash: "sha256:" + strings.Repeat("d", 64), Bytes: 12},
	}
	contract := managed.BuildReviewContract(strings.Repeat("a", 40), strings.Repeat("b", 40), "sha256:"+strings.Repeat("c", 64), guides)
	if len(contract.Guides) != 1 || contract.Guides[0] != guides[0] {
		t.Fatalf("contract.Guides = %+v, want %+v", contract.Guides, guides)
	}
	if contract.GuideInstructions == "" {
		t.Fatal("expected non-empty guide instructions when guides are configured")
	}
}

func TestBuildReviewContract_HashStableWhenGuidesUnchanged(t *testing.T) {
	guides := []managed.GuideBinding{{Path: "docs/a.md", ContentHash: "sha256:" + strings.Repeat("1", 64), Bytes: 3}}
	base := strings.Repeat("a", 40)
	input := strings.Repeat("b", 40)
	policy := "sha256:" + strings.Repeat("c", 64)

	h1, err := managed.BuildReviewContract(base, input, policy, guides).Hash()
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	h2, err := managed.BuildReviewContract(base, input, policy, guides).Hash()
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	if h1 != h2 {
		t.Fatalf("expected stable hash for unchanged guides, got %q vs %q", h1, h2)
	}
}

func TestBuildReviewContract_HashChangesWithGuideContent(t *testing.T) {
	base := strings.Repeat("a", 40)
	input := strings.Repeat("b", 40)
	policy := "sha256:" + strings.Repeat("c", 64)

	withoutGuides, err := managed.BuildReviewContract(base, input, policy, nil).Hash()
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	withGuide, err := managed.BuildReviewContract(base, input, policy, []managed.GuideBinding{
		{Path: "docs/a.md", ContentHash: "sha256:" + strings.Repeat("1", 64), Bytes: 3},
	}).Hash()
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	if withoutGuides == withGuide {
		t.Fatal("expected hash to change when a guide is added")
	}

	changedHash, err := managed.BuildReviewContract(base, input, policy, []managed.GuideBinding{
		{Path: "docs/a.md", ContentHash: "sha256:" + strings.Repeat("2", 64), Bytes: 3},
	}).Hash()
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	if withGuide == changedHash {
		t.Fatal("expected hash to change when guide content hash changes")
	}

	reordered, err := managed.BuildReviewContract(base, input, policy, []managed.GuideBinding{
		{Path: "docs/a.md", ContentHash: "sha256:" + strings.Repeat("1", 64), Bytes: 3},
		{Path: "docs/b.md", ContentHash: "sha256:" + strings.Repeat("2", 64), Bytes: 3},
	}).Hash()
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	reorderedFlip, err := managed.BuildReviewContract(base, input, policy, []managed.GuideBinding{
		{Path: "docs/b.md", ContentHash: "sha256:" + strings.Repeat("2", 64), Bytes: 3},
		{Path: "docs/a.md", ContentHash: "sha256:" + strings.Repeat("1", 64), Bytes: 3},
	}).Hash()
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	if reordered == reorderedFlip {
		t.Fatal("expected hash to change when guide order changes")
	}
}
