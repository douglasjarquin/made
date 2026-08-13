package document_test

import (
	"strings"
	"testing"

	"github.com/douglasjarquin/made/internal/agent"
	"github.com/douglasjarquin/made/internal/pipeline/document"
)

func apiDocRules() []document.Rule {
	return []document.Rule{
		{SourcePattern: "api/*", DocPattern: "docs/api/*"},
	}
}

func TestRun_PolicyViolationFlagged(t *testing.T) {
	f := setupFixture(t)
	f.pushBranch(t, "feature-violation", map[string]string{
		"api/users.go": "package api\n",
	}, "add users api")
	wt := f.addWorktree(t, "refs/heads/feature-violation")
	defer func() {
		if err := wt.Remove(); err != nil {
			t.Errorf("Remove: %v", err)
		}
	}()

	result, err := document.Run(wt.Path, "main", apiDocRules())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if len(result.Findings) != 1 {
		t.Fatalf("expected 1 finding, got %+v", result.Findings)
	}
	finding := result.Findings[0]
	if finding.Kind != agent.FindingAskUser {
		t.Fatalf("expected finding kind ask-user, got %q", finding.Kind)
	}
	if !strings.Contains(finding.Description, "api/users.go") {
		t.Fatalf("expected description to reference the violating file, got %q", finding.Description)
	}
	if !strings.Contains(finding.Description, "docs/api/*") {
		t.Fatalf("expected description to reference the violated doc pattern, got %q", finding.Description)
	}
}

func TestRun_CompliantChangeProceeds(t *testing.T) {
	f := setupFixture(t)
	f.pushBranch(t, "feature-compliant", map[string]string{
		"api/orders.go":      "package api\n",
		"docs/api/orders.md": "# orders\n",
	}, "add orders api with docs")
	wt := f.addWorktree(t, "refs/heads/feature-compliant")
	defer func() {
		if err := wt.Remove(); err != nil {
			t.Errorf("Remove: %v", err)
		}
	}()

	result, err := document.Run(wt.Path, "main", apiDocRules())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if len(result.Findings) != 0 {
		t.Fatalf("expected no findings, got %+v", result.Findings)
	}
	if !result.OK {
		t.Fatalf("expected OK=true for a compliant change, got %+v", result)
	}
}
