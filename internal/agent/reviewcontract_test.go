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

func TestReviewTask_NoGuidesOmitsGuideFieldsAndInstructions(t *testing.T) {
	input := agent.ReviewInput{
		TrustedBaseBranch: "main",
		TrustedBaseSHA:    strings.Repeat("a", 40),
		CandidateInputSHA: strings.Repeat("b", 40),
	}
	task, err := agent.NewReviewTask(input)
	if err != nil {
		t.Fatalf("NewReviewTask: %v", err)
	}
	if len(task.Contract.Guides) != 0 {
		t.Fatalf("expected no guides in contract, got %+v", task.Contract.Guides)
	}
	if task.Contract.GuideInstructions != "" {
		t.Fatalf("expected no guide instructions, got %q", task.Contract.GuideInstructions)
	}
	if strings.Contains(task.Text, "guide") {
		t.Fatalf("expected no guide mention in task text with no guides configured, got %q", task.Text)
	}
}

func TestReviewTask_GuidesEmbedPathHashByteCountAndReadCommand(t *testing.T) {
	baseSHA := strings.Repeat("a", 40)
	input := agent.ReviewInput{
		TrustedBaseBranch: "main",
		TrustedBaseSHA:    baseSHA,
		CandidateInputSHA: strings.Repeat("b", 40),
		Guides: []agent.ReviewGuideRef{
			{Path: ".made/features/README.md", ContentHash: "sha256:" + strings.Repeat("c", 64), Bytes: 42},
		},
	}
	task, err := agent.NewReviewTask(input)
	if err != nil {
		t.Fatalf("NewReviewTask: %v", err)
	}
	if len(task.Contract.Guides) != 1 {
		t.Fatalf("expected 1 guide in contract, got %+v", task.Contract.Guides)
	}
	g := task.Contract.Guides[0]
	if g.Path != ".made/features/README.md" || g.ContentHash != input.Guides[0].ContentHash || g.Bytes != 42 {
		t.Fatalf("guide contract fields = %+v, want path/hash/bytes to match input", g)
	}
	wantCommand := "git show " + baseSHA + ":.made/features/README.md"
	if g.ReadCommand != wantCommand {
		t.Fatalf("ReadCommand = %q, want %q", g.ReadCommand, wantCommand)
	}
	if task.Contract.GuideInstructions != agent.ReviewGuideInstruction {
		t.Fatalf("GuideInstructions = %q, want %q", task.Contract.GuideInstructions, agent.ReviewGuideInstruction)
	}
	if !strings.Contains(task.Text, agent.ReviewGuideInstruction) {
		t.Fatalf("task text missing guide instruction: %q", task.Text)
	}
	if !strings.Contains(task.Text, wantCommand) {
		t.Fatalf("task text missing guide read command: %q", task.Text)
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
