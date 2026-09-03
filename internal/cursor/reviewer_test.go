package cursor_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/douglasjarquin/made/internal/cursor"
)

func TestReviewerMarkdown_RejectsEmptyModel(t *testing.T) {
	_, err := cursor.ReviewerMarkdown("", nil)
	if !errors.Is(err, cursor.ErrReviewerModelRequired) {
		t.Fatalf("expected ErrReviewerModelRequired, got %v", err)
	}
}

func TestReviewerMarkdown_UsesConfiguredModelNotInherit(t *testing.T) {
	md, err := cursor.ReviewerMarkdown("claude-opus-5[effort=high,context=300k]", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(md, "model: claude-opus-5[effort=high,context=300k]") {
		t.Fatalf("expected frontmatter to contain the exact configured model, got:\n%s", md)
	}
	if strings.Contains(md, "model: inherit") {
		t.Fatalf("reviewer must never use model: inherit, got:\n%s", md)
	}
}

func TestReviewerMarkdown_IsReadOnlyForegroundReportOnly(t *testing.T) {
	md, err := cursor.ReviewerMarkdown("gpt-5", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"readonly: true", "is_background: false"} {
		if !strings.Contains(md, want) {
			t.Fatalf("expected frontmatter to contain %q, got:\n%s", want, md)
		}
	}
	if !strings.Contains(md, "Do not edit files, create commits, push, open pull requests") {
		t.Fatalf("expected report-only nonmutation instructions, got:\n%s", md)
	}
}

func TestReviewerMarkdown_ReferencesGuidePathsNotContent(t *testing.T) {
	md, err := cursor.ReviewerMarkdown("gpt-5", []string{".made/features/README.md", "docs/security.md"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(md, ".made/features/README.md") || !strings.Contains(md, "docs/security.md") {
		t.Fatalf("expected reviewer to reference configured guide paths, got:\n%s", md)
	}
	if !strings.Contains(md, "never copy their content") {
		t.Fatalf("expected reviewer to state guides are read, not copied, got:\n%s", md)
	}
}

func TestReviewerMarkdown_NoGuidesConfiguredOmitsGuideList(t *testing.T) {
	md, err := cursor.ReviewerMarkdown("gpt-5", nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(md, "Configured Review guides") {
		t.Fatalf("expected no guide list section when no guides are configured, got:\n%s", md)
	}
	if !strings.Contains(md, "follow only the entries relevant") {
		t.Fatalf("expected the selective-index-following instruction to always be present, got:\n%s", md)
	}
}

func TestReviewerMarkdown_ReferencesExternalReviewSchemaFields(t *testing.T) {
	md, err := cursor.ReviewerMarkdown("gpt-5", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{
		"schema_version", "review_contract_version", "executor", "reviewer",
		"requested_model", "actual_model", "base_sha", "input_sha",
		"policy_hash", "review_contract_hash", "findings", "guides_consulted",
	} {
		if !strings.Contains(md, field) {
			t.Fatalf("expected reviewer prompt to reference schema field %q, got:\n%s", field, md)
		}
	}
}

func TestReviewerMarkdown_IsDeterministic(t *testing.T) {
	a, err := cursor.ReviewerMarkdown("gpt-5", []string{"docs/a.md"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := cursor.ReviewerMarkdown("gpt-5", []string{"docs/a.md"})
	if err != nil {
		t.Fatal(err)
	}
	if a != b {
		t.Fatalf("expected identical output for identical input")
	}
}

func TestReviewerMarkdown_CarriesGeneratedMarker(t *testing.T) {
	md, err := cursor.ReviewerMarkdown("gpt-5", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(md, cursor.GeneratedMarker) {
		t.Fatalf("expected generated marker, got:\n%s", md)
	}
}
