package managed

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
)

var fingerprintRegexp = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

// DecisionOutcome represents the outcome of a single Decision.
type DecisionOutcome string

const (
	DecisionApproved DecisionOutcome = "approved"
	DecisionRejected DecisionOutcome = "rejected"
)

// DecisionScope captures the scope metadata for a Decision.
type DecisionScope string

const (
	ScopeOneShot              DecisionScope = "one_shot"
	ScopeSHABound             DecisionScope = "sha_bound"
	ScopeMissionFindingWaiver DecisionScope = "mission_finding_waiver"
)

// DecisionRecord is one entry in the Decisions file.
type DecisionRecord struct {
	DecisionID         string          `json:"decision_id"`
	FindingFingerprint string          `json:"finding_fingerprint"`
	Outcome            DecisionOutcome `json:"outcome"`
	Scope              DecisionScope   `json:"scope"`
	Rationale          string          `json:"rationale,omitempty"`
}

// DecisionsFile is the parsed top-level Decisions JSON structure.
type DecisionsFile struct {
	SchemaVersion int              `json:"schema_version"`
	RunID         string           `json:"run_id"`
	MissionID     string           `json:"mission_id"`
	InputSHA      string           `json:"input_sha"`
	PolicyHash    string           `json:"policy_hash"`
	Decisions     []DecisionRecord `json:"decisions"`
}

// Decisions holds the parsed and validated Decision records indexed by fingerprint.
type Decisions struct {
	byFingerprint map[string]DecisionRecord
	all           []DecisionRecord
}

// LoadDecisions parses and validates the Decisions file at path.
// opts is used to bind-check that the file matches the current run.
func LoadDecisions(path string, opts *Options) (*Decisions, error) {
	if path == "" {
		return &Decisions{byFingerprint: make(map[string]DecisionRecord)}, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("decisions: read %q: %w", path, err)
	}

	var df DecisionsFile
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&df); err != nil {
		return nil, fmt.Errorf("decisions: parse %q: %w", path, err)
	}

	if df.SchemaVersion != 1 {
		return nil, fmt.Errorf("decisions: unsupported schema_version %d (supported: 1)", df.SchemaVersion)
	}
	if df.RunID != opts.RunID {
		return nil, fmt.Errorf("decisions: run_id mismatch: file has %q, expected %q", df.RunID, opts.RunID)
	}
	if df.MissionID != opts.MissionID {
		return nil, fmt.Errorf("decisions: mission_id mismatch: file has %q, expected %q", df.MissionID, opts.MissionID)
	}
	if df.InputSHA != opts.InputSHA {
		return nil, fmt.Errorf("decisions: input_sha mismatch: file has %q, expected %q", df.InputSHA, opts.InputSHA)
	}
	if df.PolicyHash != opts.PolicyHash {
		return nil, fmt.Errorf("decisions: policy_hash mismatch: file has %q, expected %q", df.PolicyHash, opts.PolicyHash)
	}

	byFingerprint := make(map[string]DecisionRecord, len(df.Decisions))
	seenIDs := make(map[string]struct{}, len(df.Decisions))

	for _, d := range df.Decisions {
		if d.DecisionID == "" {
			return nil, fmt.Errorf("decisions: empty decision_id")
		}
		if _, dup := seenIDs[d.DecisionID]; dup {
			return nil, fmt.Errorf("decisions: duplicate decision_id %q", d.DecisionID)
		}
		seenIDs[d.DecisionID] = struct{}{}

		if !fingerprintRegexp.MatchString(d.FindingFingerprint) {
			return nil, fmt.Errorf("decisions: decision %q has malformed fingerprint %q", d.DecisionID, d.FindingFingerprint)
		}
		if d.Outcome != DecisionApproved && d.Outcome != DecisionRejected {
			return nil, fmt.Errorf("decisions: decision %q has invalid outcome %q", d.DecisionID, d.Outcome)
		}
		switch d.Scope {
		case ScopeOneShot, ScopeSHABound, ScopeMissionFindingWaiver:
		case "":
			return nil, fmt.Errorf("decisions: decision %q has empty scope", d.DecisionID)
		default:
			return nil, fmt.Errorf("decisions: decision %q has unknown scope %q", d.DecisionID, d.Scope)
		}

		if existing, exists := byFingerprint[d.FindingFingerprint]; exists {
			if existing.Outcome != d.Outcome {
				return nil, fmt.Errorf("decisions: conflicting outcomes for fingerprint %q: %q vs %q", d.FindingFingerprint, existing.Outcome, d.Outcome)
			}
		}
		byFingerprint[d.FindingFingerprint] = d
	}

	return &Decisions{byFingerprint: byFingerprint, all: df.Decisions}, nil
}

// Lookup returns the Decision for a fingerprint, or (zero, false) if none.
func (d *Decisions) Lookup(fingerprint string) (DecisionRecord, bool) {
	rec, ok := d.byFingerprint[fingerprint]
	return rec, ok
}

// All returns all Decision records (for reporting unused decisions).
func (d *Decisions) All() []DecisionRecord {
	return append([]DecisionRecord(nil), d.all...)
}

// MarkUsed records that a fingerprint was matched against a finding.
func (d *Decisions) MarkUsed(fingerprint string) {
	// Used decisions are tracked by callers via separate set for now;
	// this is a hook for future per-Decision usage tracking.
}
