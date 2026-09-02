package review_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/douglasjarquin/made/internal/agent"
	"github.com/douglasjarquin/made/internal/agent/agenttest"
	"github.com/douglasjarquin/made/internal/pipeline/review"
)

type recordingEvidenceStore struct {
	files map[string][]byte
}

func (s *recordingEvidenceStore) WriteEvidence(_ string, files map[string][]byte) error {
	s.files = make(map[string][]byte, len(files))
	for name, content := range files {
		s.files[name] = append([]byte(nil), content...)
	}
	return nil
}

func TestRun_WritesVersionedReviewEvidenceWithCandidateOutputSHA(t *testing.T) {
	bin := agenttest.Build(t)
	f := setupFixture(t)
	wt := f.addWorktree(t)
	t.Cleanup(func() { _ = wt.Remove() })
	scenarioPath := writeScenario(t, agent.Findings{})
	store := &recordingEvidenceStore{}
	candidateOutputSHA := headSHA(t, wt.Path)

	result, err := review.Run(context.Background(), wt.Path, agent.KindCodex, review.Options{
		BinaryPath:         bin,
		BaseBranch:         "HEAD",
		CandidateOutputSHA: candidateOutputSHA,
		Evidence:           store,
		EvidenceRunID:      "run-review-evidence",
		ExtraEnv: []string{
			"FAKE_AGENT_KIND=codex",
			"FAKE_AGENT_SCENARIO=" + scenarioPath,
		},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !result.OK {
		t.Fatalf("review result = %+v, want OK", result)
	}

	metadata, ok := store.files["review-contract.json"]
	if !ok {
		t.Fatalf("review evidence files = %v, want review-contract.json", store.files)
	}
	var contract struct {
		PromptVersion       string `json:"prompt_version"`
		OutputSchemaVersion string `json:"output_schema_version"`
		TrustedBaseSHA      string `json:"trusted_base_sha"`
		CandidateInputSHA   string `json:"candidate_input_sha"`
		CandidateOutputSHA  string `json:"candidate_output_sha"`
	}
	if err := json.Unmarshal(metadata, &contract); err != nil {
		t.Fatalf("decode review-contract.json: %v", err)
	}
	if contract.PromptVersion == "" || contract.OutputSchemaVersion == "" || contract.TrustedBaseSHA == "" || contract.CandidateInputSHA == "" {
		t.Fatalf("review contract metadata missing identity/version fields: %+v", contract)
	}
	if contract.CandidateOutputSHA != candidateOutputSHA {
		t.Fatalf("candidate output SHA = %q, want %q", contract.CandidateOutputSHA, candidateOutputSHA)
	}
	if _, ok := store.files["review-prompt.txt"]; !ok {
		t.Fatalf("review evidence omitted review-prompt.txt: %v", store.files)
	}
	if string(store.files["review-response.json"]) != `{"findings":[]}` {
		t.Fatalf("review response = %q, want structured empty findings", store.files["review-response.json"])
	}
}

func TestRun_PassesConfiguredGuidesIntoReviewContract(t *testing.T) {
	bin := agenttest.Build(t)
	f := setupFixture(t)
	wt := f.addWorktree(t)
	t.Cleanup(func() { _ = wt.Remove() })
	scenarioPath := writeScenario(t, agent.Findings{})
	store := &recordingEvidenceStore{}
	baseSHA := headSHA(t, wt.Path)

	_, err := review.Run(context.Background(), wt.Path, agent.KindCodex, review.Options{
		BinaryPath:    bin,
		BaseBranch:    "HEAD",
		Evidence:      store,
		EvidenceRunID: "run-review-guides",
		Guides: []agent.ReviewGuideRef{
			{Path: ".made/features/README.md", ContentHash: "sha256:" + fixtureGuideHash, Bytes: 7},
		},
		ExtraEnv: []string{
			"FAKE_AGENT_KIND=codex",
			"FAKE_AGENT_SCENARIO=" + scenarioPath,
		},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	metadata, ok := store.files["review-contract.json"]
	if !ok {
		t.Fatalf("review evidence files = %v, want review-contract.json", store.files)
	}
	var contract struct {
		Guides []struct {
			Path        string `json:"path"`
			ContentHash string `json:"content_hash"`
			Bytes       int    `json:"bytes"`
			ReadCommand string `json:"read_command"`
		} `json:"guides"`
		GuideInstructions string `json:"guide_instructions"`
	}
	if err := json.Unmarshal(metadata, &contract); err != nil {
		t.Fatalf("decode review-contract.json: %v", err)
	}
	if len(contract.Guides) != 1 {
		t.Fatalf("expected 1 guide in review-contract.json, got %+v", contract.Guides)
	}
	g := contract.Guides[0]
	if g.Path != ".made/features/README.md" || g.ContentHash != "sha256:"+fixtureGuideHash || g.Bytes != 7 {
		t.Fatalf("guide metadata = %+v, want the configured path/hash/bytes", g)
	}
	if g.ReadCommand != "git show "+baseSHA+":.made/features/README.md" {
		t.Fatalf("ReadCommand = %q, want git show %s:.made/features/README.md", g.ReadCommand, baseSHA)
	}
	if contract.GuideInstructions == "" {
		t.Fatal("expected non-empty guide_instructions when guides are configured")
	}
	prompt, ok := store.files["review-prompt.txt"]
	if !ok || !strings.Contains(string(prompt), g.ReadCommand) {
		t.Fatalf("review-prompt.txt = %q, want it to include the guide read command", prompt)
	}
}

const fixtureGuideHash = "0000000000000000000000000000000000000000000000000000000000000000000"
