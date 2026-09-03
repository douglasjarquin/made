package managed

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
)

const (
	ExternalReviewSchemaVersion  = 1
	ReviewContractVersion        = 1
	maxExternalReviewBytes       = 1 << 20
	maxExternalReviewFindings    = 200
	maxExternalReviewStringBytes = 2000
	maxExternalReviewPatchBytes  = 65536
	maxExternalReviewPathBytes   = 1024
)

var validExternalFindingKinds = map[string]struct{}{
	string("auto-fixable"): {},
	string("ask-user"):     {},
	string("blocking"):     {},
}

// ExternalReviewIdentity is the exact identity an external review result
// must be bound to before its findings are trusted.
type ExternalReviewIdentity struct {
	BaseSHA      string
	InputSHA     string
	PolicyHash   string
	ContractHash string
}

// ExternalReviewResult is Made's strict, bounded, versioned schema for a
// caller-supplied Review result (see issue #39's "External review-result
// contract"). Unknown fields are rejected so a future field always requires
// a version bump, not silent tolerance.
type ExternalReviewResult struct {
	SchemaVersion         int               `json:"schema_version"`
	ReviewContractVersion int               `json:"review_contract_version"`
	Executor              string            `json:"executor"`
	Reviewer              string            `json:"reviewer"`
	RequestedModel        string            `json:"requested_model"`
	ActualModel           string            `json:"actual_model"`
	BaseSHA               string            `json:"base_sha"`
	InputSHA              string            `json:"input_sha"`
	PolicyHash            string            `json:"policy_hash"`
	ReviewContractHash    string            `json:"review_contract_hash"`
	Findings              []ExternalFinding `json:"findings"`
}

// ExternalFinding is one finding inside an ExternalReviewResult. Its shape
// mirrors agent.Finding's managed-mode fields exactly, so internal and
// external findings normalize through the same fingerprint/classification
// path with no field translation.
type ExternalFinding struct {
	Kind        string   `json:"kind"`
	Description string   `json:"description"`
	Code        string   `json:"code"`
	Class       string   `json:"class"`
	Symbol      string   `json:"symbol,omitempty"`
	Paths       []string `json:"paths"`
	Patch       string   `json:"patch,omitempty"`
}

// ParseExternalReviewResult reads path once as a bounded regular file,
// strictly decodes it, and validates it against want before returning any
// finding. A malformed or oversized result is never echoed back wholesale.
func ParseExternalReviewResult(path string, want ExternalReviewIdentity) (ExternalReviewResult, error) {
	data, err := readBoundedRegularFile(path, maxExternalReviewBytes)
	if err != nil {
		return ExternalReviewResult{}, fmt.Errorf("managed: read external review result: %w", err)
	}

	var result ExternalReviewResult
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil {
		return ExternalReviewResult{}, fmt.Errorf("managed: parse external review result: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return ExternalReviewResult{}, fmt.Errorf("managed: external review result must contain exactly one JSON document")
	}

	if err := validateExternalReviewResult(result, want); err != nil {
		return ExternalReviewResult{}, err
	}
	return result, nil
}

func validateExternalReviewResult(r ExternalReviewResult, want ExternalReviewIdentity) error {
	if r.SchemaVersion != ExternalReviewSchemaVersion {
		return fmt.Errorf("managed: external review result schema_version %d is not supported (want %d)", r.SchemaVersion, ExternalReviewSchemaVersion)
	}
	if r.ReviewContractVersion != ReviewContractVersion {
		return fmt.Errorf("managed: external review result review_contract_version %d is not supported (want %d)", r.ReviewContractVersion, ReviewContractVersion)
	}
	if r.BaseSHA != want.BaseSHA {
		return fmt.Errorf("managed: external review result base_sha %q does not match %q", r.BaseSHA, want.BaseSHA)
	}
	if r.InputSHA != want.InputSHA {
		return fmt.Errorf("managed: external review result input_sha %q does not match %q", r.InputSHA, want.InputSHA)
	}
	if r.PolicyHash != want.PolicyHash {
		return fmt.Errorf("managed: external review result policy_hash %q does not match %q", r.PolicyHash, want.PolicyHash)
	}
	if r.ReviewContractHash != want.ContractHash {
		return fmt.Errorf("managed: external review result review_contract_hash %q does not match %q", r.ReviewContractHash, want.ContractHash)
	}
	if err := boundString("executor", r.Executor, maxExternalReviewStringBytes); err != nil {
		return err
	}
	if err := boundString("reviewer", r.Reviewer, maxExternalReviewStringBytes); err != nil {
		return err
	}
	if err := boundString("requested_model", r.RequestedModel, maxExternalReviewStringBytes); err != nil {
		return err
	}
	if err := boundString("actual_model", r.ActualModel, maxExternalReviewStringBytes); err != nil {
		return err
	}
	if len(r.Findings) > maxExternalReviewFindings {
		return fmt.Errorf("managed: external review result has %d findings, exceeding the maximum of %d", len(r.Findings), maxExternalReviewFindings)
	}

	seen := make(map[string]ExternalFinding, len(r.Findings))
	for i, f := range r.Findings {
		if _, ok := validExternalFindingKinds[f.Kind]; !ok {
			return fmt.Errorf("managed: external review result finding[%d] has invalid kind %q", i, f.Kind)
		}
		if err := boundString(fmt.Sprintf("finding[%d].description", i), f.Description, maxExternalReviewStringBytes); err != nil {
			return err
		}
		if err := boundString(fmt.Sprintf("finding[%d].code", i), f.Code, maxExternalReviewStringBytes); err != nil {
			return err
		}
		if err := boundString(fmt.Sprintf("finding[%d].class", i), f.Class, maxExternalReviewStringBytes); err != nil {
			return err
		}
		if err := boundString(fmt.Sprintf("finding[%d].symbol", i), f.Symbol, maxExternalReviewStringBytes); err != nil {
			return err
		}
		if len(f.Patch) > maxExternalReviewPatchBytes {
			return fmt.Errorf("managed: external review result finding[%d].patch exceeds %d bytes", i, maxExternalReviewPatchBytes)
		}
		for j, p := range f.Paths {
			if len(p) > maxExternalReviewPathBytes {
				return fmt.Errorf("managed: external review result finding[%d].paths[%d] exceeds %d bytes", i, j, maxExternalReviewPathBytes)
			}
		}
		identityErr := ValidateStableFindingIdentity(FingerprintInput{
			Stage:  stageReview,
			Kind:   f.Kind,
			Code:   f.Code,
			Class:  f.Class,
			Symbol: f.Symbol,
			Paths:  f.Paths,
		})
		if identityErr != nil {
			return fmt.Errorf("managed: external review result finding[%d]: %w", i, identityErr)
		}

		identity := Fingerprint(FingerprintInput{
			Stage:  stageReview,
			Code:   f.Code,
			Class:  f.Class,
			Symbol: f.Symbol,
			Paths:  f.Paths,
		})
		if existing, dup := seen[identity]; dup {
			if existing.Kind != f.Kind {
				return fmt.Errorf("managed: external review result finding[%d] conflicts with an earlier finding sharing structural identity %s but a different kind", i, identity)
			}
			return fmt.Errorf("managed: external review result finding[%d] duplicates an earlier finding (structural identity %s)", i, identity)
		}
		seen[identity] = f
	}
	return nil
}

func boundString(label, value string, max int) error {
	if len(value) > max {
		return fmt.Errorf("managed: external review result %s exceeds %d bytes", label, max)
	}
	return nil
}

func readBoundedRegularFile(path string, limit int64) ([]byte, error) {
	fi, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !fi.Mode().IsRegular() {
		return nil, fmt.Errorf("%q is not a regular file", path)
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	stat, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !stat.Mode().IsRegular() {
		return nil, fmt.Errorf("%q is not a regular file after open", path)
	}
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("%q exceeds %d bytes", path, limit)
	}
	return data, nil
}
