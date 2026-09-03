package cursor_test

import (
	"strings"
	"testing"

	"github.com/douglasjarquin/made/internal/cursor"
)

func TestSkillMarkdown_IsDeterministic(t *testing.T) {
	first := cursor.SkillMarkdown()
	second := cursor.SkillMarkdown()
	if first != second {
		t.Fatal("expected identical output across calls")
	}
}

func TestSkillMarkdown_ReferencesVerifyCommandSurface(t *testing.T) {
	md := cursor.SkillMarkdown()
	for _, want := range []string{
		"made cursor doctor --json",
		"made verify prepare --executor cursor",
		"made verify complete --request",
		"made verify run --base-ref",
		"made-reviewer",
	} {
		if !strings.Contains(md, want) {
			t.Fatalf("expected skill to reference %q, got:\n%s", want, md)
		}
	}
}

func TestSkillMarkdown_ForbidsDaemonGatePushPRCIMerge(t *testing.T) {
	md := cursor.SkillMarkdown()
	if !strings.Contains(md, "Never") {
		t.Fatalf("expected an explicit prohibition section, got:\n%s", md)
	}
	for _, forbidden := range []string{"start the Made daemon", "made gate init", "push a branch", "open a pull request", "poll CI", "merge"} {
		if !strings.Contains(md, forbidden) {
			t.Fatalf("expected skill to explicitly forbid %q, got:\n%s", forbidden, md)
		}
	}
}

func TestSkillMarkdown_DocumentsOutcomeHandling(t *testing.T) {
	md := cursor.SkillMarkdown()
	for _, outcome := range []string{"failed_retryable", "needs_decision", "failed_terminal", "infrastructure_error", "canceled", "passed"} {
		if !strings.Contains(md, outcome) {
			t.Fatalf("expected skill to document outcome %q, got:\n%s", outcome, md)
		}
	}
}

func TestSkillMarkdown_ForbidsRebuildingMadePerInvocation(t *testing.T) {
	md := cursor.SkillMarkdown()
	if !strings.Contains(md, "Do not clone,") || !strings.Contains(md, "rebuild Made from source on every skill invocation") {
		t.Fatalf("expected skill to forbid rebuilding Made per invocation, got:\n%s", md)
	}
}

func TestSkillMarkdown_HasMinimalFrontmatter(t *testing.T) {
	md := cursor.SkillMarkdown()
	if !strings.HasPrefix(md, "---\nname: "+cursor.SkillName+"\n") {
		t.Fatalf("expected frontmatter to start with name, got:\n%s", md)
	}
	if !strings.Contains(md, "description: ") {
		t.Fatalf("expected a description field, got:\n%s", md)
	}
}

func TestSkillMarkdown_CarriesGeneratedMarker(t *testing.T) {
	if !strings.Contains(cursor.SkillMarkdown(), cursor.GeneratedMarker) {
		t.Fatal("expected generated marker")
	}
}
