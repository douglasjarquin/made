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
		"made cursor doctor --base-ref",
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

func TestSkillMarkdown_ReviewBranchKeysOnDoctorDetailNotStatus(t *testing.T) {
	md := cursor.SkillMarkdown()
	if !strings.Contains(md, "`detail`") {
		t.Fatalf("expected the review-branch step to key off cursor_executor's detail field, got:\n%s", md)
	}
	if strings.Contains(md, "status `configured`") {
		t.Fatalf("expected the review-branch step to stop referring to a literal status of `configured` (doctor.go always sets status ok/warn/skipped; the configured/not_configured strings live in detail), got:\n%s", md)
	}
}

func TestSkillMarkdown_ReviewBranchHandlesDoctorWarnByStopping(t *testing.T) {
	md := cursor.SkillMarkdown()
	if !strings.Contains(md, "warn") {
		t.Fatalf("expected a third branch for cursor_executor status warn, got:\n%s", md)
	}
	if !strings.Contains(md, "review.required") {
		t.Fatalf("expected the warn branch to explain the review.required-but-no-model condition, got:\n%s", md)
	}
}

func TestSkillMarkdown_DoctorInvocationIncludesBaseRef(t *testing.T) {
	md := cursor.SkillMarkdown()
	if !strings.Contains(md, "made cursor doctor --base-ref") {
		t.Fatalf("expected made cursor doctor to pass --base-ref so the base_ref check actually runs, got:\n%s", md)
	}
}

func TestSkillMarkdown_InstallationReferencesPinnedInstallScript(t *testing.T) {
	md := cursor.SkillMarkdown()
	if !strings.Contains(md, "scripts/install-cursor-cloud.sh") {
		t.Fatalf("expected the Installation section to name scripts/install-cursor-cloud.sh, got:\n%s", md)
	}
}

func TestSkillMarkdown_DocumentsNoNativeSubagentFallback(t *testing.T) {
	md := cursor.SkillMarkdown()
	for _, want := range []string{
		"no built-in mechanism",
		"frontmatter and body",
		"verbatim",
	} {
		if !strings.Contains(md, want) {
			t.Fatalf("expected a no-native-subagent-invocation fallback mentioning %q, got:\n%s", want, md)
		}
	}
}
