package managed_test

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// makeTestRepo creates a minimal Git repo at dir with a commit on main
// and returns (repoDir, baseSHA, inputSHA).
func makeTestRepo(t *testing.T) (dir, baseSHA, inputSHA string) {
	t.Helper()
	dir = t.TempDir()

	gitCmds := [][]string{
		{"init", "-b", "main"},
		{"config", "user.email", "test@test.local"},
		{"config", "user.name", "test"},
		{"config", "commit.gpgsign", "false"},
	}
	for _, args := range gitCmds {
		c := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}

	// Base commit.
	writeFile(t, filepath.Join(dir, "README.md"), "# test\n")
	for _, args := range [][]string{
		{"add", "."},
		{"commit", "-m", "initial"},
	} {
		c := exec.Command("git", append([]string{"-C", dir, "-c", "commit.gpgsign=false"}, args...)...)
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	baseSHA = gitRevParse(t, dir, "HEAD")

	// Input commit.
	writeFile(t, filepath.Join(dir, "hello.go"), "package main\n")
	for _, args := range [][]string{
		{"add", "."},
		{"commit", "-m", "add hello.go"},
	} {
		c := exec.Command("git", append([]string{"-C", dir, "-c", "commit.gpgsign=false"}, args...)...)
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	inputSHA = gitRevParse(t, dir, "HEAD")
	return
}

func gitRevParse(t *testing.T, dir, ref string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", dir, "rev-parse", "--verify", ref).Output()
	if err != nil {
		t.Fatalf("git rev-parse %s: %v", ref, err)
	}
	return strings.TrimSpace(string(out))
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func makeTrustedConfig(t *testing.T, content string) (path, hash string) {
	t.Helper()
	dir := t.TempDir()
	path = filepath.Join(dir, ".made.yml")
	writeFile(t, path, content)
	data, _ := os.ReadFile(path)
	sum := sha256.Sum256(data)
	hash = "sha256:" + hex.EncodeToString(sum[:])
	return
}

func makeEvidenceDir(t *testing.T) string {
	t.Helper()
	return t.TempDir()
}

// minimalConfig is a valid .made.yml for tests that don't need commands.
const minimalConfig = `version: 1
commands:
  test: "true"
  lint: "true"
`

// parseEvents parses JSON-Lines from a buffer and returns the events as maps.
func parseEvents(t *testing.T, data []byte) []map[string]any {
	t.Helper()
	var events []map[string]any
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var ev map[string]any
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			t.Fatalf("parse event line %q: %v", line, err)
		}
		events = append(events, ev)
	}
	return events
}

// findTerminalEvent returns the run.completed event or fails.
func findTerminalEvent(t *testing.T, events []map[string]any) map[string]any {
	t.Helper()
	for _, ev := range events {
		if ev["event"] == "run.completed" {
			return ev
		}
	}
	t.Fatal("no run.completed event found")
	return nil
}

// assertProtocolInvariants checks the protocol contract on a stream of events.
func assertProtocolInvariants(t *testing.T, events []map[string]any, opts struct {
	RunID      string
	MissionID  string
	InputSHA   string
	PolicyHash string
}) {
	t.Helper()

	if len(events) == 0 {
		t.Fatal("no events emitted")
	}

	// sequence starts at 1 and is contiguous
	for i, ev := range events {
		seq := int(ev["sequence"].(float64))
		if seq != i+1 {
			t.Errorf("event[%d]: expected sequence %d, got %d", i, i+1, seq)
		}
	}

	// schema_version and protocol_version are constant
	for i, ev := range events {
		if sv := int(ev["schema_version"].(float64)); sv != 1 {
			t.Errorf("event[%d]: schema_version=%d, want 1", i, sv)
		}
		if pv := int(ev["protocol_version"].(float64)); pv != 1 {
			t.Errorf("event[%d]: protocol_version=%d, want 1", i, pv)
		}
	}

	// run identity is constant
	for i, ev := range events {
		if ev["run_id"] != opts.RunID {
			t.Errorf("event[%d]: run_id=%q, want %q", i, ev["run_id"], opts.RunID)
		}
		if ev["mission_id"] != opts.MissionID {
			t.Errorf("event[%d]: mission_id=%q, want %q", i, ev["mission_id"], opts.MissionID)
		}
		if ev["input_sha"] != opts.InputSHA {
			t.Errorf("event[%d]: input_sha=%q, want %q", i, ev["input_sha"], opts.InputSHA)
		}
		if ev["policy_hash"] != opts.PolicyHash {
			t.Errorf("event[%d]: policy_hash=%q, want %q", i, ev["policy_hash"], opts.PolicyHash)
		}
	}

	// exactly one terminal event
	termCount := 0
	lastTermIdx := -1
	for i, ev := range events {
		if ev["event"] == "run.completed" {
			termCount++
			lastTermIdx = i
		}
	}
	if termCount != 1 {
		t.Errorf("expected exactly 1 run.completed event, got %d", termCount)
	}
	if lastTermIdx != len(events)-1 {
		t.Errorf("run.completed must be the last event, but it is at index %d of %d", lastTermIdx, len(events)-1)
	}

	// timestamps are parseable
	for i, ev := range events {
		ts, ok := ev["timestamp"].(string)
		if !ok || ts == "" {
			t.Errorf("event[%d]: missing or non-string timestamp", i)
			continue
		}
		if _, err := time.Parse(time.RFC3339Nano, ts); err != nil {
			t.Errorf("event[%d]: timestamp %q not RFC3339Nano: %v", i, ts, err)
		}
	}
}

// terminalOutcome extracts the outcome string from the run.completed payload.
func terminalOutcome(t *testing.T, events []map[string]any) string {
	t.Helper()
	term := findTerminalEvent(t, events)
	payload, ok := term["payload"].(map[string]any)
	if !ok {
		t.Fatal("run.completed payload is not an object")
	}
	outcome, ok := payload["outcome"].(string)
	if !ok {
		t.Fatal("run.completed payload.outcome is not a string")
	}
	return outcome
}

// runManagedValidate executes `made validate --managed` with the given args
// and returns (stdout bytes, stderr bytes, exit code).
func runManagedValidate(t *testing.T, extraArgs ...string) ([]byte, []byte, int) {
	t.Helper()
	// Build the binary first.
	binPath := filepath.Join(t.TempDir(), "made")
	build := exec.Command("go", "build", "-o", binPath, "github.com/douglasjarquin/made/cmd/made")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build made: %v: %s", err, out)
	}

	var stdout, stderr bytes.Buffer
	cmd := exec.Command(binPath, extraArgs...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	code := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			code = exitErr.ExitCode()
		} else {
			t.Fatalf("run made: %v", err)
		}
	}
	return stdout.Bytes(), stderr.Bytes(), code
}

// fullValidateArgs constructs a complete set of args for made validate --managed.
func fullValidateArgs(runID, missionID, workspace, baseSHA, inputSHA, configPath, policyHash, evidenceDir string, extra ...string) []string {
	args := []string{
		"validate", "--managed", "--json-events",
		"--run-id", runID,
		"--mission-id", missionID,
		"--workspace", workspace,
		"--base-sha", baseSHA,
		"--input-sha", inputSHA,
		"--trusted-config", configPath,
		"--policy-hash", policyHash,
		"--evidence-dir", evidenceDir,
	}
	return append(args, extra...)
}

// TestProtocol_SequenceStartsAt1AndIsContiguous verifies basic protocol invariants
// on a passing run.
func TestProtocol_SequenceStartsAt1AndIsContiguous(t *testing.T) {
	workspace, baseSHA, inputSHA := makeTestRepo(t)
	configPath, policyHash := makeTrustedConfig(t, minimalConfig)
	evidenceDir := makeEvidenceDir(t)

	stdout, _, code := runManagedValidate(t, fullValidateArgs(
		"G-1", "M-1", workspace, baseSHA, inputSHA, configPath, policyHash, evidenceDir,
	)...)

	events := parseEvents(t, stdout)
	assertProtocolInvariants(t, events, struct {
		RunID, MissionID, InputSHA, PolicyHash string
	}{"G-1", "M-1", inputSHA, policyHash})

	outcome := terminalOutcome(t, events)
	// With no review agent, review stage will fail as infrastructure_error
	// (no agent configured), but protocol invariants must still hold.
	t.Logf("outcome=%s exit=%d", outcome, code)
}

// TestProtocol_StdoutContainsOnlyValidJSONLines verifies that every line is valid JSON.
func TestProtocol_StdoutContainsOnlyValidJSONLines(t *testing.T) {
	workspace, baseSHA, inputSHA := makeTestRepo(t)
	configPath, policyHash := makeTrustedConfig(t, minimalConfig)
	evidenceDir := makeEvidenceDir(t)

	stdout, _, _ := runManagedValidate(t, fullValidateArgs(
		"G-2", "M-2", workspace, baseSHA, inputSHA, configPath, policyHash, evidenceDir,
	)...)

	scanner := bufio.NewScanner(bytes.NewReader(stdout))
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var v any
		if err := json.Unmarshal([]byte(line), &v); err != nil {
			t.Errorf("line %d is not valid JSON: %v\nline: %s", lineNo, err, line)
		}
	}
	if lineNo == 0 {
		t.Error("no output lines at all")
	}
}

// TestProtocol_ExactlyOneTerminalEvent ensures exactly one run.completed event.
func TestProtocol_ExactlyOneTerminalEvent(t *testing.T) {
	workspace, baseSHA, inputSHA := makeTestRepo(t)
	configPath, policyHash := makeTrustedConfig(t, minimalConfig)
	evidenceDir := makeEvidenceDir(t)

	stdout, _, _ := runManagedValidate(t, fullValidateArgs(
		"G-3", "M-3", workspace, baseSHA, inputSHA, configPath, policyHash, evidenceDir,
	)...)

	events := parseEvents(t, stdout)
	count := 0
	for _, ev := range events {
		if ev["event"] == "run.completed" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected exactly 1 run.completed, got %d", count)
	}
}

// TestProtocol_RunStartedIsFirst verifies the first event is run.started.
func TestProtocol_RunStartedIsFirst(t *testing.T) {
	workspace, baseSHA, inputSHA := makeTestRepo(t)
	configPath, policyHash := makeTrustedConfig(t, minimalConfig)
	evidenceDir := makeEvidenceDir(t)

	stdout, _, _ := runManagedValidate(t, fullValidateArgs(
		"G-4", "M-4", workspace, baseSHA, inputSHA, configPath, policyHash, evidenceDir,
	)...)

	events := parseEvents(t, stdout)
	if len(events) == 0 {
		t.Fatal("no events")
	}
	if events[0]["event"] != "run.started" {
		t.Errorf("first event is %q, want %q", events[0]["event"], "run.started")
	}
}

// TestProtocol_ExitCodeMatchesOutcome verifies exit code contract.
func TestProtocol_ExitCodeMatchesOutcome(t *testing.T) {
	workspace, baseSHA, inputSHA := makeTestRepo(t)
	// Use a config with no agent to force infrastructure_error on review stage.
	configPath, policyHash := makeTrustedConfig(t, minimalConfig)
	evidenceDir := makeEvidenceDir(t)

	stdout, _, code := runManagedValidate(t, fullValidateArgs(
		"G-5", "M-5", workspace, baseSHA, inputSHA, configPath, policyHash, evidenceDir,
	)...)

	events := parseEvents(t, stdout)
	outcome := terminalOutcome(t, events)

	expectedCode := map[string]int{
		"passed":               0,
		"infrastructure_error": 1,
		"needs_decision":       3,
		"failed_retryable":     4,
		"failed_terminal":      5,
		"canceled":             130,
	}[outcome]

	if code != expectedCode {
		t.Errorf("outcome %q: expected exit code %d, got %d", outcome, expectedCode, code)
	}
}

// TestPreflight_RelativeWorkspaceRejected verifies preflight rejects relative paths.
func TestPreflight_RelativeWorkspaceRejected(t *testing.T) {
	workspace, baseSHA, inputSHA := makeTestRepo(t)
	configPath, policyHash := makeTrustedConfig(t, minimalConfig)
	evidenceDir := makeEvidenceDir(t)

	_, stderr, code := runManagedValidate(t,
		"validate", "--managed", "--json-events",
		"--run-id", "G-10",
		"--mission-id", "M-10",
		"--workspace", "relative/path", // relative — should fail
		"--base-sha", baseSHA,
		"--input-sha", inputSHA,
		"--trusted-config", configPath,
		"--policy-hash", policyHash,
		"--evidence-dir", evidenceDir,
	)

	if code == 0 {
		t.Error("expected non-zero exit for relative workspace")
	}
	if !strings.Contains(string(stderr), "not an absolute path") &&
		!strings.Contains(string(stderr), "preflight") {
		t.Errorf("expected preflight error in stderr, got: %s", stderr)
	}
	_ = workspace // only needed to build valid config
}

// TestPreflight_WrongHeadRejected verifies that a workspace with a different HEAD is rejected.
func TestPreflight_WrongHeadRejected(t *testing.T) {
	workspace, baseSHA, inputSHA := makeTestRepo(t)
	_ = inputSHA
	configPath, policyHash := makeTrustedConfig(t, minimalConfig)
	evidenceDir := makeEvidenceDir(t)

	wrongSHA := strings.Repeat("a", 40)
	_, stderr, code := runManagedValidate(t, fullValidateArgs(
		"G-11", "M-11", workspace, baseSHA, wrongSHA, configPath, policyHash, evidenceDir,
	)...)

	if code == 0 {
		t.Error("expected non-zero exit for wrong input_sha")
	}
	if !strings.Contains(string(stderr), "preflight") {
		t.Logf("stderr: %s", stderr)
	}
}

// TestPreflight_AbbreviatedSHARejected verifies abbreviated SHAs are rejected.
func TestPreflight_AbbreviatedSHARejected(t *testing.T) {
	workspace, baseSHA, inputSHA := makeTestRepo(t)
	configPath, policyHash := makeTrustedConfig(t, minimalConfig)
	evidenceDir := makeEvidenceDir(t)

	_, _, code := runManagedValidate(t, fullValidateArgs(
		"G-12", "M-12", workspace, baseSHA, inputSHA[:8], configPath, policyHash, evidenceDir,
	)...)
	if code == 0 {
		t.Error("expected non-zero exit for abbreviated input_sha")
	}
}

// TestPreflight_ConfigHashMismatchRejected verifies hash mismatch is rejected.
func TestPreflight_ConfigHashMismatchRejected(t *testing.T) {
	workspace, baseSHA, inputSHA := makeTestRepo(t)
	configPath, _ := makeTrustedConfig(t, minimalConfig)
	evidenceDir := makeEvidenceDir(t)

	wrongHash := "sha256:" + strings.Repeat("0", 64)
	_, stderr, code := runManagedValidate(t, fullValidateArgs(
		"G-13", "M-13", workspace, baseSHA, inputSHA, configPath, wrongHash, evidenceDir,
	)...)
	if code == 0 {
		t.Error("expected non-zero exit for hash mismatch")
	}
	if !strings.Contains(string(stderr), "hash mismatch") && !strings.Contains(string(stderr), "preflight") {
		t.Logf("stderr: %s", stderr)
	}
}

// TestPreflight_EvidenceDirInsideWorkspaceRejected verifies evidence dir placement.
func TestPreflight_EvidenceDirInsideWorkspaceRejected(t *testing.T) {
	workspace, baseSHA, inputSHA := makeTestRepo(t)
	configPath, policyHash := makeTrustedConfig(t, minimalConfig)

	_, _, code := runManagedValidate(t, fullValidateArgs(
		"G-14", "M-14", workspace, baseSHA, inputSHA, configPath, policyHash,
		filepath.Join(workspace, "evidence"), // inside workspace — should fail
	)...)
	if code == 0 {
		t.Error("expected non-zero exit for evidence dir inside workspace")
	}
}

// TestPreflight_NonAncestorBaseRejected verifies that base must be an ancestor.
func TestPreflight_NonAncestorBaseRejected(t *testing.T) {
	workspace, baseSHA, inputSHA := makeTestRepo(t)
	_ = baseSHA
	configPath, policyHash := makeTrustedConfig(t, minimalConfig)
	evidenceDir := makeEvidenceDir(t)

	// Use inputSHA as base and baseSHA as input — reversed, so base is a descendant.
	// We need a different non-ancestor SHA. Use inputSHA as both (it's an ancestor of itself,
	// so we need to create a sibling commit).
	// Create a branch commit that is not an ancestor of HEAD.
	c := exec.Command("git", "-C", workspace, "-c", "commit.gpgsign=false",
		"commit-tree", "HEAD^{tree}", "-p", inputSHA, "-m", "sibling")
	out, err := c.Output()
	if err != nil {
		t.Skipf("cannot create sibling commit: %v", err)
	}
	sibSHA := strings.TrimSpace(string(out))

	_, _, code := runManagedValidate(t, fullValidateArgs(
		"G-15", "M-15", workspace, sibSHA, inputSHA, configPath, policyHash, evidenceDir,
	)...)
	if code == 0 {
		t.Error("expected non-zero exit for non-ancestor base SHA")
	}
}

// TestPreflight_DirtyWorkspaceRejected verifies that uncommitted changes are rejected.
func TestPreflight_DirtyWorkspaceRejected(t *testing.T) {
	workspace, baseSHA, inputSHA := makeTestRepo(t)
	configPath, policyHash := makeTrustedConfig(t, minimalConfig)
	evidenceDir := makeEvidenceDir(t)

	// Make the workspace dirty.
	writeFile(t, filepath.Join(workspace, "dirty.txt"), "dirty\n")

	_, _, code := runManagedValidate(t, fullValidateArgs(
		"G-16", "M-16", workspace, baseSHA, inputSHA, configPath, policyHash, evidenceDir,
	)...)
	if code == 0 {
		t.Error("expected non-zero exit for dirty workspace")
	}
}

// TestPreflight_MissingRequiredFlagReturns2 verifies exit 2 for usage errors.
func TestPreflight_MissingRequiredFlagReturns2(t *testing.T) {
	_, _, code := runManagedValidate(t, "validate", "--managed", "--json-events",
		"--run-id", "G-20",
		// missing --mission-id and others
	)
	if code != 2 {
		t.Errorf("expected exit 2 for missing flags, got %d", code)
	}
}

// TestCapabilities_IncludesManagedV1 verifies capabilities advertises validate.managed.v1.
func TestCapabilities_IncludesManagedV1(t *testing.T) {
	binPath := filepath.Join(t.TempDir(), "made")
	build := exec.Command("go", "build", "-o", binPath, "github.com/douglasjarquin/made/cmd/made")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build made: %v: %s", err, out)
	}

	out, err := exec.Command(binPath, "capabilities", "--json").Output()
	if err != nil {
		t.Fatalf("capabilities: %v", err)
	}
	var report struct {
		Commands []string `json:"commands"`
	}
	if err := json.Unmarshal(out, &report); err != nil {
		t.Fatalf("parse capabilities: %v", err)
	}
	found := false
	for _, cmd := range report.Commands {
		if cmd == "validate.managed.v1" {
			found = true
		}
	}
	if !found {
		t.Errorf("validate.managed.v1 not in capabilities commands: %v", report.Commands)
	}
}

// TestProtocol_RunIDIsEchoedExactly verifies opaque run_id passthrough.
func TestProtocol_RunIDIsEchoedExactly(t *testing.T) {
	workspace, baseSHA, inputSHA := makeTestRepo(t)
	configPath, policyHash := makeTrustedConfig(t, minimalConfig)
	evidenceDir := makeEvidenceDir(t)

	opaqueRunID := "G-run-9999-opaque-string-!@#"
	stdout, _, _ := runManagedValidate(t, fullValidateArgs(
		opaqueRunID, "M-opaque", workspace, baseSHA, inputSHA, configPath, policyHash, evidenceDir,
	)...)

	events := parseEvents(t, stdout)
	for i, ev := range events {
		if ev["run_id"] != opaqueRunID {
			t.Errorf("event[%d]: run_id=%q, want %q", i, ev["run_id"], opaqueRunID)
		}
	}
}

// TestDecisions_WrongRunIDRejectedAtPreflight verifies Decisions file binding.
func TestDecisions_WrongRunIDRejectedAtPreflight(t *testing.T) {
	workspace, baseSHA, inputSHA := makeTestRepo(t)
	configPath, policyHash := makeTrustedConfig(t, minimalConfig)
	evidenceDir := makeEvidenceDir(t)

	// Write a decisions file with a mismatched run_id.
	decPath := filepath.Join(t.TempDir(), "decisions.json")
	decContent := fmt.Sprintf(`{
		"schema_version": 1,
		"run_id": "WRONG-RUN-ID",
		"mission_id": "M-dec",
		"input_sha": %q,
		"policy_hash": %q,
		"decisions": []
	}`, inputSHA, policyHash)
	writeFile(t, decPath, decContent)

	_, _, code := runManagedValidate(t, append(
		fullValidateArgs("G-dec", "M-dec", workspace, baseSHA, inputSHA, configPath, policyHash, evidenceDir),
		"--decisions", decPath,
	)...)
	if code == 0 {
		t.Error("expected non-zero exit for mismatched run_id in decisions file")
	}
}

// TestDecisions_WrongInputSHARejected verifies stale Decisions are rejected.
func TestDecisions_WrongInputSHARejected(t *testing.T) {
	workspace, baseSHA, inputSHA := makeTestRepo(t)
	configPath, policyHash := makeTrustedConfig(t, minimalConfig)
	evidenceDir := makeEvidenceDir(t)

	decPath := filepath.Join(t.TempDir(), "decisions.json")
	wrongSHA := strings.Repeat("f", 40)
	decContent := fmt.Sprintf(`{
		"schema_version": 1,
		"run_id": "G-dec2",
		"mission_id": "M-dec2",
		"input_sha": %q,
		"policy_hash": %q,
		"decisions": []
	}`, wrongSHA, policyHash)
	writeFile(t, decPath, decContent)

	_, _, code := runManagedValidate(t, append(
		fullValidateArgs("G-dec2", "M-dec2", workspace, baseSHA, inputSHA, configPath, policyHash, evidenceDir),
		"--decisions", decPath,
	)...)
	if code == 0 {
		t.Error("expected non-zero exit for mismatched input_sha in decisions file")
	}
}

// TestEvidenceDir_WrittenOutsideWorkspace verifies evidence appears in evidence dir.
func TestEvidenceDir_WrittenOutsideWorkspace(t *testing.T) {
	workspace, baseSHA, inputSHA := makeTestRepo(t)
	configPath, policyHash := makeTrustedConfig(t, minimalConfig)
	evidenceDir := makeEvidenceDir(t)

	runManagedValidate(t, fullValidateArgs(
		"G-ev", "M-ev", workspace, baseSHA, inputSHA, configPath, policyHash, evidenceDir,
	)...)

	// Evidence directory must have the run-id subdirectory.
	runDir := filepath.Join(evidenceDir, "G-ev")
	info, err := os.Stat(runDir)
	if err != nil {
		t.Fatalf("evidence run dir %q not created: %v", runDir, err)
	}
	if !info.IsDir() {
		t.Errorf("evidence run dir %q is not a directory", runDir)
	}

	// No evidence should be inside the workspace.
	entries, _ := os.ReadDir(workspace)
	for _, e := range entries {
		if e.Name() != ".git" && e.Name() != "README.md" && e.Name() != "hello.go" {
			t.Errorf("unexpected file in workspace: %s", e.Name())
		}
	}
}
