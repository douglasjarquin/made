package agent_test

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/douglasjarquin/made/internal/agent"
)

func TestReviewTask_EmbedsVersionedIdentityScopeAndTaxonomy(t *testing.T) {
	input := agent.ReviewInput{
		TrustedBaseBranch:  "main",
		TrustedBaseSHA:     strings.Repeat("a", 40),
		CandidateInputSHA:  strings.Repeat("b", 40),
		CandidateOutputSHA: strings.Repeat("c", 40),
	}

	task, err := agent.NewReviewTask(input)
	if err != nil {
		t.Fatalf("NewReviewTask: %v", err)
	}

	if task.Contract.PromptVersion != agent.ReviewPromptVersion {
		t.Fatalf("prompt version = %q, want %q", task.Contract.PromptVersion, agent.ReviewPromptVersion)
	}
	if task.Contract.OutputSchemaVersion != agent.ReviewOutputSchemaVersion {
		t.Fatalf("output schema version = %q, want %q", task.Contract.OutputSchemaVersion, agent.ReviewOutputSchemaVersion)
	}
	if task.Contract.TrustedBaseSHA != input.TrustedBaseSHA || task.Contract.CandidateInputSHA != input.CandidateInputSHA || task.Contract.CandidateOutputSHA != input.CandidateOutputSHA {
		t.Fatalf("task identity = %+v, want input identity %+v", task.Contract, input)
	}
	if task.Contract.DiffCommand == "" || !strings.Contains(task.Contract.DiffCommand, input.TrustedBaseSHA) || !strings.Contains(task.Contract.DiffCommand, input.CandidateInputSHA) {
		t.Fatalf("diff command = %q, want both exact commit identities", task.Contract.DiffCommand)
	}
	if len(task.Contract.FindingTaxonomy) < 8 || len(task.Contract.FindingKinds) != 3 {
		t.Fatalf("review taxonomy = %+v, want substantive taxonomy and three finding kinds", task.Contract)
	}

	const marker = "MADE_REVIEW_CONTRACT="
	lineStart := strings.Index(task.Text, marker)
	if lineStart < 0 {
		t.Fatalf("review task omitted machine-readable contract marker: %q", task.Text)
	}
	line := task.Text[lineStart+len(marker):]
	if newline := strings.IndexByte(line, '\n'); newline >= 0 {
		line = line[:newline]
	}
	var embedded agent.ReviewContract
	if err := json.Unmarshal([]byte(line), &embedded); err != nil {
		t.Fatalf("decode embedded review contract: %v", err)
	}
	if !reflect.DeepEqual(embedded, task.Contract) {
		t.Fatalf("embedded contract = %+v, want %+v", embedded, task.Contract)
	}
}

func TestReviewTask_RejectsMissingTrustedBaseIdentity(t *testing.T) {
	_, err := agent.NewReviewTask(agent.ReviewInput{
		TrustedBaseBranch: "main",
		CandidateInputSHA: strings.Repeat("b", 40),
	})
	if err == nil || !strings.Contains(err.Error(), "trusted base SHA") {
		t.Fatalf("NewReviewTask error = %v, want actionable trusted-base error", err)
	}
}

func TestManagedReviewTask_DefinedForStrictStructuralIdentity(t *testing.T) {
	input := agent.ReviewInput{
		TrustedBaseBranch:  "main",
		TrustedBaseSHA:     strings.Repeat("a", 40),
		CandidateInputSHA:  strings.Repeat("b", 40),
		CandidateOutputSHA: strings.Repeat("c", 40),
	}

	task, err := agent.NewManagedReviewTask(input)
	if err != nil {
		t.Fatalf("NewManagedReviewTask: %v", err)
	}

	// Managed task must have same versions and identity as standard task
	if task.Contract.PromptVersion != agent.ReviewPromptVersion {
		t.Fatalf("prompt version = %q, want %q", task.Contract.PromptVersion, agent.ReviewPromptVersion)
	}
	if task.Contract.OutputSchemaVersion != agent.ReviewOutputSchemaVersion {
		t.Fatalf("output schema version = %q, want %q", task.Contract.OutputSchemaVersion, agent.ReviewOutputSchemaVersion)
	}
	if task.Contract.TrustedBaseSHA != input.TrustedBaseSHA || task.Contract.CandidateInputSHA != input.CandidateInputSHA {
		t.Fatalf("task identity mismatch")
	}

	// Managed task must emphasize finding-specific code requirement
	if !strings.Contains(task.Text, "Managed-validation mode") {
		t.Fatalf("managed prompt missing mode marker")
	}
	if !strings.Contains(task.Text, "finding-specific") {
		t.Fatalf("managed prompt missing finding-specific requirement")
	}
	if !strings.Contains(task.Text, "code") || !strings.Contains(task.Text, "class") {
		t.Fatalf("managed prompt missing code or class requirements")
	}

	// Managed task must include repository-relative path requirements
	if !strings.Contains(task.Text, "repository-relative") {
		t.Fatalf("managed prompt missing repository-relative path requirement")
	}

	// Verify contract marker is present and valid
	const marker = "MADE_REVIEW_CONTRACT="
	lineStart := strings.Index(task.Text, marker)
	if lineStart < 0 {
		t.Fatalf("managed review task omitted contract marker: %q", task.Text)
	}
	line := task.Text[lineStart+len(marker):]
	if newline := strings.IndexByte(line, '\n'); newline >= 0 {
		line = line[:newline]
	}
	var embedded agent.ReviewContract
	if err := json.Unmarshal([]byte(line), &embedded); err != nil {
		t.Fatalf("decode managed embedded review contract: %v", err)
	}
	if !reflect.DeepEqual(embedded, task.Contract) {
		t.Fatalf("managed embedded contract mismatch")
	}
}

