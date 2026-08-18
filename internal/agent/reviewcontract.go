package agent

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
)

const (
	ReviewPromptVersion       = "made-review-prompt-v1"
	ReviewOutputSchemaVersion = "made-review-schema-v1"
	maxReviewTaskBytes        = 256 << 10
)

var reviewFindingTaxonomy = []string{
	"correctness",
	"data-loss",
	"security-trust-boundary",
	"concurrency-lifecycle",
	"public-api-schema-compatibility",
	"error-handling",
	"material-performance-regression",
	"missing-tests",
	"documentation-public-contract",
}

var reviewFindingKinds = []string{"auto-fixable", "ask-user", "blocking"}

type ReviewInput struct {
	TrustedBaseBranch  string
	TrustedBaseSHA     string
	CandidateInputSHA  string
	CandidateOutputSHA string
}

type ReviewContract struct {
	PromptVersion       string   `json:"prompt_version"`
	OutputSchemaVersion string   `json:"output_schema_version"`
	TrustedBaseBranch   string   `json:"trusted_base_branch"`
	TrustedBaseSHA      string   `json:"trusted_base_sha"`
	CandidateInputSHA   string   `json:"candidate_input_sha"`
	CandidateOutputSHA  string   `json:"candidate_output_sha,omitempty"`
	DiffCommand         string   `json:"diff_command"`
	Scope               string   `json:"scope"`
	FindingTaxonomy     []string `json:"finding_taxonomy"`
	FindingKinds        []string `json:"finding_kinds"`
	FindingPolicy       []string `json:"finding_policy"`
	Exclusions          []string `json:"exclusions"`
}

type ReviewTask struct {
	Contract ReviewContract
	Text     string
}

func NewReviewTask(input ReviewInput) (ReviewTask, error) {
	baseBranch := strings.TrimSpace(input.TrustedBaseBranch)
	if baseBranch == "" {
		return ReviewTask{}, fmt.Errorf("agent: trusted base branch is required")
	}
	if strings.ContainsAny(baseBranch, "\r\n") {
		return ReviewTask{}, fmt.Errorf("agent: trusted base branch contains a newline")
	}
	if err := validateReviewSHA("trusted base SHA", input.TrustedBaseSHA); err != nil {
		return ReviewTask{}, err
	}
	if err := validateReviewSHA("candidate input SHA", input.CandidateInputSHA); err != nil {
		return ReviewTask{}, err
	}
	if input.CandidateOutputSHA != "" {
		if err := validateReviewSHA("candidate output SHA", input.CandidateOutputSHA); err != nil {
			return ReviewTask{}, err
		}
	}

	contract := ReviewContract{
		PromptVersion:       ReviewPromptVersion,
		OutputSchemaVersion: ReviewOutputSchemaVersion,
		TrustedBaseBranch:   baseBranch,
		TrustedBaseSHA:      input.TrustedBaseSHA,
		CandidateInputSHA:   input.CandidateInputSHA,
		CandidateOutputSHA:  input.CandidateOutputSHA,
		DiffCommand:         fmt.Sprintf("git diff --no-ext-diff --unified=80 %s..%s --", input.TrustedBaseSHA, input.CandidateInputSHA),
		Scope:               "Review only defects introduced by the candidate change relative to the trusted base, including directly affected behavior and public interfaces.",
		FindingTaxonomy:     append([]string(nil), reviewFindingTaxonomy...),
		FindingKinds:        append([]string(nil), reviewFindingKinds...),
		FindingPolicy: []string{
			"auto-fixable: a fully specified mechanical patch for an introduced defect with exact tracked affected paths; Made applies it only after controlled path, index, and validation checks.",
			"ask-user: a finding that requires human judgment or a policy choice; preserve it for explicit approval and never apply a patch automatically.",
			"blocking: an introduced defect that makes delivery unsafe; halt the stage and never apply a patch automatically.",
		},
		Exclusions: []string{
			"unrelated legacy defects",
			"general style advice",
			"broad refactoring suggestions",
		},
	}
	contractJSON, err := json.Marshal(contract)
	if err != nil {
		return ReviewTask{}, fmt.Errorf("agent: encode review contract: %w", err)
	}
	text := "Inspect the exact candidate diff before deciding that findings are empty. " +
		"Return only the structured object matching the supplied output schema.\n" +
		"MADE_REVIEW_CONTRACT=" + string(contractJSON) + "\n" +
		"Review every taxonomy category that applies, report exact affected paths for every patch, " +
		"and do not report excluded material.\n"
	if len([]byte(text)) > maxReviewTaskBytes {
		return ReviewTask{}, fmt.Errorf("agent: review task exceeds %d bytes", maxReviewTaskBytes)
	}
	return ReviewTask{Contract: contract, Text: text}, nil
}

func validateReviewSHA(label, value string) error {
	if len(value) < 40 || len(value) > 64 {
		return fmt.Errorf("agent: %s must be a full Git commit SHA", label)
	}
	if _, err := hex.DecodeString(value); err != nil {
		return fmt.Errorf("agent: %s must be a hexadecimal Git commit SHA: %w", label, err)
	}
	return nil
}
