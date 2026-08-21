package managed

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// TestFixturesAreValid ensures all checked-in JSONL and Decision fixtures are
// faithful to the protocol contract. This prevents documentation drift.
func TestFixturesAreValid(t *testing.T) {
	testdataDir := "testdata"

	// Validate all JSONL fixtures
	jsonlFiles := []string{
		"passed.jsonl",
		"needs-decision.jsonl",
		"failed-retryable.jsonl",
		"failed-terminal.jsonl",
		"infrastructure-error.jsonl",
	}

	for _, fixture := range jsonlFiles {
		t.Run("jsonl/"+fixture, func(t *testing.T) {
			validateJSONLFixture(t, filepath.Join(testdataDir, fixture))
		})
	}

	// Validate all Decision fixtures
	decisionFiles := []string{
		"decisions-approved.json",
		"decisions-rejected.json",
	}

	for _, fixture := range decisionFiles {
		t.Run("decision/"+fixture, func(t *testing.T) {
			validateDecisionFixture(t, filepath.Join(testdataDir, fixture))
		})
	}
}

// validateJSONLFixture checks a JSONL event stream fixture for protocol validity.
func validateJSONLFixture(t *testing.T, path string) {
	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("failed to open fixture: %v", err)
	}
	defer func() { _ = file.Close() }()

	scanner := bufio.NewScanner(file)
	var (
		lastSequence   int
		runID          string
		missionID      string
		invocationID   string
		inputSHA       string
		baseSHA        string
		policyHash     string
		terminalCount  int
		seenRunStarted bool
	)

	// Regex for validating hash formats
	sha1Regex := regexp.MustCompile(`^[0-9a-f]{40}$`)
	sha256Regex := regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

	lineNum := 0
	for scanner.Scan() {
		lineNum++
		var envelope map[string]interface{}
		if err := json.Unmarshal(scanner.Bytes(), &envelope); err != nil {
			t.Errorf("line %d: malformed JSON: %v", lineNum, err)
			continue
		}

		// Validate required envelope fields
		schemaVersion, ok := envelope["schema_version"].(float64)
		if !ok || int(schemaVersion) != 1 {
			t.Errorf("line %d: missing or incorrect schema_version", lineNum)
		}

		protocolVersion, ok := envelope["protocol_version"].(float64)
		if !ok || int(protocolVersion) != 1 {
			t.Errorf("line %d: missing or incorrect protocol_version", lineNum)
		}

		sequence, ok := envelope["sequence"].(float64)
		if !ok {
			t.Errorf("line %d: missing sequence", lineNum)
			continue
		}

		eventType, ok := envelope["event"].(string)
		if !ok {
			t.Errorf("line %d: missing event type", lineNum)
			continue
		}

		// Check sequence continuity
		expectedSeq := lastSequence + 1
		if int(sequence) != expectedSeq {
			t.Errorf("line %d: sequence %d, expected %d", lineNum, int(sequence), expectedSeq)
		}
		lastSequence = int(sequence)

		// Validate and check constancy of run/invocation identifiers
		rid, ok := envelope["run_id"].(string)
		if !ok || rid == "" {
			t.Errorf("line %d: missing or empty run_id", lineNum)
			continue
		}
		if runID == "" {
			runID = rid
		} else if rid != runID {
			t.Errorf("line %d: run_id mismatch: %s vs %s", lineNum, rid, runID)
		}

		mid, ok := envelope["mission_id"].(string)
		if !ok || mid == "" {
			t.Errorf("line %d: missing or empty mission_id", lineNum)
			continue
		}
		if missionID == "" {
			missionID = mid
		} else if mid != missionID {
			t.Errorf("line %d: mission_id mismatch: %s vs %s", lineNum, mid, missionID)
		}

		iid, ok := envelope["invocation_id"].(string)
		if !ok || iid == "" {
			t.Errorf("line %d: missing or empty invocation_id", lineNum)
			continue
		}
		if invocationID == "" {
			invocationID = iid
		} else if iid != invocationID {
			t.Errorf("line %d: invocation_id mismatch: %s vs %s", lineNum, iid, invocationID)
		}

		isha, ok := envelope["input_sha"].(string)
		if !ok || !sha1Regex.MatchString(isha) {
			t.Errorf("line %d: invalid input_sha: %q (expected 40-hex)", lineNum, isha)
			continue
		}
		if inputSHA == "" {
			inputSHA = isha
		} else if isha != inputSHA {
			t.Errorf("line %d: input_sha mismatch: %s vs %s", lineNum, isha, inputSHA)
		}

		bsha, ok := envelope["base_sha"].(string)
		if !ok || !sha1Regex.MatchString(bsha) {
			t.Errorf("line %d: invalid base_sha: %q (expected 40-hex)", lineNum, bsha)
			continue
		}
		if baseSHA == "" {
			baseSHA = bsha
		} else if bsha != baseSHA {
			t.Errorf("line %d: base_sha mismatch: %s vs %s", lineNum, bsha, baseSHA)
		}

		ph, ok := envelope["policy_hash"].(string)
		if !ok || !sha256Regex.MatchString(ph) {
			t.Errorf("line %d: invalid policy_hash: %q (expected sha256:<64-hex>)", lineNum, ph)
			continue
		}
		if policyHash == "" {
			policyHash = ph
		} else if ph != policyHash {
			t.Errorf("line %d: policy_hash mismatch: %s vs %s", lineNum, ph, policyHash)
		}

		// Validate timestamp format (basic check)
		_, ok = envelope["timestamp"].(string)
		if !ok {
			t.Errorf("line %d: missing or non-string timestamp", lineNum)
		}

		// Track terminal events
		if eventType == "run.completed" {
			terminalCount++
		}

		// First event must be run.started
		if lineNum == 1 && eventType != "run.started" {
			t.Errorf("line %d: first event must be run.started, got %s", lineNum, eventType)
		}

		// Validate run.started timing
		if eventType == "run.started" {
			if lineNum != 1 {
				t.Errorf("line %d: run.started must be first event, found at line %d", 1, lineNum)
			}
			seenRunStarted = true
		}
	}

	if err := scanner.Err(); err != nil {
		t.Fatalf("scanner error: %v", err)
	}

	if !seenRunStarted {
		t.Error("fixture missing run.started event")
	}

	if terminalCount != 1 {
		t.Errorf("expected exactly 1 terminal event (run.completed), got %d", terminalCount)
	}

	if lastSequence < 1 {
		t.Error("fixture contains no events")
	}
}

// validateDecisionFixture checks a Decision JSON fixture for validity.
func validateDecisionFixture(t *testing.T, path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read fixture: %v", err)
	}

	// Parse DecisionsFile directly to validate JSON structure
	var df DecisionsFile
	if err := json.Unmarshal(data, &df); err != nil {
		t.Fatalf("failed to parse decisions JSON: %v", err)
	}

	// Check that all required fields exist and have expected formats
	sha1Regex := regexp.MustCompile(`^[0-9a-f]{40}$`)
	sha256Regex := regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

	if df.SchemaVersion != 1 {
		t.Errorf("incorrect schema_version: %d (expected 1)", df.SchemaVersion)
	}
	if df.RunID == "" {
		t.Error("missing run_id")
	}
	if df.MissionID == "" {
		t.Error("missing mission_id")
	}
	if !sha1Regex.MatchString(df.InputSHA) {
		t.Errorf("invalid input_sha: %q (expected 40-hex)", df.InputSHA)
	}
	if !sha1Regex.MatchString(df.BaseSHA) {
		t.Errorf("invalid base_sha: %q (expected 40-hex)", df.BaseSHA)
	}
	if !sha256Regex.MatchString(df.PolicyHash) {
		t.Errorf("invalid policy_hash: %q (expected sha256:<64-hex>)", df.PolicyHash)
	}
	if df.InvocationID == "" {
		t.Error("missing invocation_id")
	}

	// Check for duplicate decision IDs
	seenIDs := make(map[string]bool)
	for _, d := range df.Decisions {
		if d.DecisionID == "" {
			t.Error("decision missing decision_id")
		}
		if seenIDs[d.DecisionID] {
			t.Errorf("duplicate decision_id: %s", d.DecisionID)
		}
		seenIDs[d.DecisionID] = true

		// Validate fingerprint format
		if !sha256Regex.MatchString(d.FindingFingerprint) {
			t.Errorf("invalid fingerprint: %q (expected sha256:<64-hex>)", d.FindingFingerprint)
		}

		// Validate outcome
		if d.Outcome != DecisionApproved && d.Outcome != DecisionRejected {
			t.Errorf("invalid outcome: %q (expected approved or rejected)", d.Outcome)
		}

		// Validate scope
		validScopes := map[DecisionScope]bool{
			ScopeOneShot:              true,
			ScopeSHABound:             true,
			ScopeMissionFindingWaiver: true,
		}
		if !validScopes[d.Scope] {
			t.Errorf("invalid scope: %q", d.Scope)
		}
	}
}
