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

// ReviewGuideInstruction is the exact, bounded instruction given to a
// reviewer for selectively using configured guides (project issue #40):
// Made never eagerly concatenates every guide into the review prompt.
const ReviewGuideInstruction = "Read every configured top-level guide. When a guide is an index, follow only the entries relevant to the exact base-to-input change unless the change is broad enough to require a full sweep."

// ReviewGuideRef is a caller-resolved guide's identity (path, exact content
// hash, byte count) handed to NewReviewTask/NewManagedReviewTask, which turn
// it into a ReviewGuide with a read command bound to this task's trusted
// base SHA.
type ReviewGuideRef struct {
	Path        string
	ContentHash string
	Bytes       int
}

// ReviewGuide is one configured guide as embedded in the review contract:
// its trusted identity plus a bounded, safe command for reading its exact
// bytes from the trusted base.
type ReviewGuide struct {
	Path        string `json:"path"`
	ContentHash string `json:"content_hash"`
	Bytes       int    `json:"bytes"`
	ReadCommand string `json:"read_command"`
}

type ReviewInput struct {
	TrustedBaseBranch  string
	TrustedBaseSHA     string
	CandidateInputSHA  string
	CandidateOutputSHA string
	Guides             []ReviewGuideRef
}

type ReviewContract struct {
	PromptVersion       string        `json:"prompt_version"`
	OutputSchemaVersion string        `json:"output_schema_version"`
	TrustedBaseBranch   string        `json:"trusted_base_branch"`
	TrustedBaseSHA      string        `json:"trusted_base_sha"`
	CandidateInputSHA   string        `json:"candidate_input_sha"`
	CandidateOutputSHA  string        `json:"candidate_output_sha,omitempty"`
	DiffCommand         string        `json:"diff_command"`
	Scope               string        `json:"scope"`
	FindingTaxonomy     []string      `json:"finding_taxonomy"`
	FindingKinds        []string      `json:"finding_kinds"`
	FindingPolicy       []string      `json:"finding_policy"`
	Exclusions          []string      `json:"exclusions"`
	Guides              []ReviewGuide `json:"guides,omitempty"`
	GuideInstructions   string        `json:"guide_instructions,omitempty"`
}

func buildContractGuides(trustedBaseSHA string, refs []ReviewGuideRef) []ReviewGuide {
	if len(refs) == 0 {
		return nil
	}
	guides := make([]ReviewGuide, 0, len(refs))
	for _, ref := range refs {
		guides = append(guides, ReviewGuide{
			Path:        ref.Path,
			ContentHash: ref.ContentHash,
			Bytes:       ref.Bytes,
			ReadCommand: fmt.Sprintf("git show %s:%s", trustedBaseSHA, ref.Path),
		})
	}
	return guides
}

func guideTaskText(guides []ReviewGuide, instructions string) string {
	if len(guides) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(instructions)
	b.WriteString("\nConfigured guides (trusted, exact bytes, read via each guide's read_command):\n")
	for _, g := range guides {
		fmt.Fprintf(&b, "- %s (content_hash=%s, bytes=%d): %s\n", g.Path, g.ContentHash, g.Bytes, g.ReadCommand)
	}
	return b.String()
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
	contract.Guides = buildContractGuides(input.TrustedBaseSHA, input.Guides)
	if len(contract.Guides) > 0 {
		contract.GuideInstructions = ReviewGuideInstruction
	}
	contractJSON, err := json.Marshal(contract)
	if err != nil {
		return ReviewTask{}, fmt.Errorf("agent: encode review contract: %w", err)
	}
	text := "Inspect the exact candidate diff before deciding that findings are empty. " +
		"Return only the structured object matching the supplied output schema.\n" +
		"MADE_REVIEW_CONTRACT=" + string(contractJSON) + "\n" +
		"Review every taxonomy category that applies, report exact affected paths for every patch, " +
		"and do not report excluded material.\n" +
		guideTaskText(contract.Guides, contract.GuideInstructions)
	if len([]byte(text)) > maxReviewTaskBytes {
		return ReviewTask{}, fmt.Errorf("agent: review task exceeds %d bytes", maxReviewTaskBytes)
	}
	return ReviewTask{Contract: contract, Text: text}, nil
}

// NewManagedReviewTask builds a review task for managed-validation mode.
// Managed mode has stricter requirements than standalone review:
//   - Every finding must include a stable, finding-specific code (e.g., "sql_injection", not "security")
//   - Every finding must include a class from the finding taxonomy
//   - Every finding must include repository-relative paths (normalized, no ".." or ".")
//   - Multi-finding files must include symbol or locus to disambiguate
//
// This ensures Decisions can be reliably reapplied across runs and paraphrases.
func NewManagedReviewTask(input ReviewInput) (ReviewTask, error) {
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
	contract.Guides = buildContractGuides(input.TrustedBaseSHA, input.Guides)
	if len(contract.Guides) > 0 {
		contract.GuideInstructions = ReviewGuideInstruction
	}
	contractJSON, err := json.Marshal(contract)
	if err != nil {
		return ReviewTask{}, fmt.Errorf("agent: encode managed review contract: %w", err)
	}
	text := "Managed-validation mode: Every finding MUST include stable structural identity. " +
		"Inspect the exact candidate diff before deciding that findings are empty. " +
		"Return only the structured object matching the supplied output schema.\n" +
		"MADE_REVIEW_CONTRACT=" + string(contractJSON) + "\n" +
		"MANAGED MODE REQUIREMENTS:\n" +
		"- code: A stable, finding-specific identifier (e.g., 'sql_injection', not 'security'). Must uniquely identify this class of defect within the file. The same code describes the same defect across paraphrases and reruns.\n" +
		"- class: One of the finding_taxonomy values, indicating the category of this defect.\n" +
		"- paths: Nonempty array of repository-relative file paths affected by this finding (e.g., ['src/auth.go', 'src/token.go']). Paths must be normalized (no '..' or '.' components).\n" +
		"- symbol (when multiple findings affect the same file): Stable locus or function name to disambiguate (e.g., 'validateToken', 'line 42').\n" +
		"Review every taxonomy category that applies, report exact affected paths for every patch, " +
		"include complete structural identity for every finding, " +
		"and do not report excluded material.\n" +
		guideTaskText(contract.Guides, contract.GuideInstructions)
	if len([]byte(text)) > maxReviewTaskBytes {
		return ReviewTask{}, fmt.Errorf("agent: managed review task exceeds %d bytes", maxReviewTaskBytes)
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
