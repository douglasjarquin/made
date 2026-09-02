package managed

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

var reviewContractTaxonomy = []string{
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

var reviewContractFindingKinds = []string{"auto-fixable", "ask-user", "blocking"}

var reviewContractNonmutationRules = []string{
	"a reviewer must never apply a patch, create a commit, or otherwise modify the workspace",
	"a reviewer must review the exact base_sha..input_sha diff and no other change",
}

var reviewContractAuthorityRules = []string{
	"only this contract's schema and taxonomy define a valid Made Review; a caller may render it for its own reviewer but may not alter it and still claim a Made Review",
	"Made classifies findings; a reviewer reports them but does not decide the terminal outcome",
}

// ReviewContract is Made's one canonical, versioned Review request: what a
// reviewer (internal or external) commits to reviewing under, whatever
// executor produces the result. Its Hash binds an external result to this
// exact contract, so a caller cannot silently narrow or reinterpret it.
type ReviewContract struct {
	SchemaVersion    int      `json:"schema_version"`
	Taxonomy         []string `json:"taxonomy"`
	FindingKinds     []string `json:"finding_kinds"`
	BaseSHA          string   `json:"base_sha"`
	InputSHA         string   `json:"input_sha"`
	PolicyHash       string   `json:"policy_hash"`
	DiffInstructions string   `json:"diff_instructions"`
	NonmutationRules []string `json:"nonmutation_rules"`
	AuthorityRules   []string `json:"authority_rules"`
	GuidePaths       []string `json:"guide_paths,omitempty"`
}

// BuildReviewContract constructs the canonical contract for one run. Guide
// paths/hashes (issue #40) are not implemented; GuidePaths is a clean,
// always-empty extension point reserved for that later issue.
func BuildReviewContract(baseSHA, inputSHA, policyHash string) ReviewContract {
	return ReviewContract{
		SchemaVersion:    ReviewContractVersion,
		Taxonomy:         append([]string(nil), reviewContractTaxonomy...),
		FindingKinds:     append([]string(nil), reviewContractFindingKinds...),
		BaseSHA:          baseSHA,
		InputSHA:         inputSHA,
		PolicyHash:       policyHash,
		DiffInstructions: fmt.Sprintf("git diff --no-ext-diff --unified=80 %s..%s --", baseSHA, inputSHA),
		NonmutationRules: append([]string(nil), reviewContractNonmutationRules...),
		AuthorityRules:   append([]string(nil), reviewContractAuthorityRules...),
	}
}

// Hash returns the contract's stable sha256:<hex> identity. A caller
// rendering this contract for an external reviewer computes the same value
// independently from the documented schema and these exact inputs.
func (c ReviewContract) Hash() (string, error) {
	data, err := json.Marshal(c)
	if err != nil {
		return "", fmt.Errorf("managed: hash review contract: %w", err)
	}
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}
