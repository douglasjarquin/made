package managed_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/douglasjarquin/made/internal/agent"
	"github.com/douglasjarquin/made/internal/agent/agenttest"
	"github.com/douglasjarquin/made/internal/managed"
)

// agentConfig is a .made.yml that enables the codex agent (overridden in tests
// with a fake agent binary via Options.ReviewAgentBinaryPath).
const agentConfig = `version: 1
commands:
  test: "true"
  lint: "true"
agent: codex
`

type e2eResult struct {
	exitCode int
	events   []map[string]any
	stdout   []byte
	stderr   []byte
}

// runManaged invokes managed.Run directly using OS pipes for stdout/stderr and
// returns the parsed JSON-Lines event stream.
func runManaged(t *testing.T, ctx context.Context, opts *managed.Options) e2eResult {
	t.Helper()

	stdoutR, stdoutW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	stderrR, stderrW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}

	// Drain both pipes concurrently to avoid deadlock if output exceeds the
	// pipe buffer.
	var stdoutBuf, stderrBuf bytes.Buffer
	done := make(chan struct{}, 2)
	go func() { _, _ = io.Copy(&stdoutBuf, stdoutR); done <- struct{}{} }()
	go func() { _, _ = io.Copy(&stderrBuf, stderrR); done <- struct{}{} }()

	exitCode := managed.Run(ctx, opts, stdoutW, stderrW)
	_ = stdoutW.Close()
	_ = stderrW.Close()
	<-done
	<-done
	_ = stdoutR.Close()
	_ = stderrR.Close()

	var events []map[string]any
	scanner := bufio.NewScanner(bytes.NewReader(stdoutBuf.Bytes()))
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var ev map[string]any
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			t.Fatalf("unmarshal event %q: %v", line, err)
		}
		events = append(events, ev)
	}

	return e2eResult{
		exitCode: exitCode,
		events:   events,
		stdout:   stdoutBuf.Bytes(),
		stderr:   stderrBuf.Bytes(),
	}
}

// e2eOptions builds a fully-populated Options for a passing/failing e2e run,
// wiring the fake agent binary and scenario into the review stage.
func e2eOptions(t *testing.T, runID, missionID string, findings agent.Findings) *managed.Options {
	t.Helper()
	workspace, baseSHA, inputSHA := makeTestRepo(t)
	configPath, policyHash := makeTrustedConfig(t, agentConfig)
	evidenceDir := makeEvidenceDir(t)
	scenario := writeScenario(t, findings)
	bin := agenttest.Build(t)

	return &managed.Options{
		RunID:                 runID,
		MissionID:             missionID,
		Workspace:             workspace,
		BaseSHA:               baseSHA,
		InputSHA:              inputSHA,
		TrustedConfig:         configPath,
		PolicyHash:            policyHash,
		EvidenceDir:           evidenceDir,
		ReviewAgentBinaryPath: bin,
		ReviewAgentExtraEnv:   []string{"FAKE_AGENT_SCENARIO=" + scenario},
	}
}

// writeScenario writes a fakeagent scenario file containing the given findings.
func writeScenario(t *testing.T, findings agent.Findings) string {
	t.Helper()
	data, err := json.Marshal(findings)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "scenario.json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func finding(kind agent.FindingKind, code, class, desc string, paths ...string) agent.Finding {
	return agent.Finding{
		Kind:        kind,
		Code:        code,
		Class:       class,
		Description: desc,
		Paths:       paths,
	}
}

// autoFixFinding builds an auto-fixable finding with a placeholder patch. In
// managed (ReportOnly) mode the patch is never applied, but the agent contract
// requires auto-fixable findings to carry a non-empty patch.
func autoFixFinding(code, class, desc string, paths ...string) agent.Finding {
	f := finding(agent.FindingAutoFixable, code, class, desc, paths...)
	f.Patch = "--- a/feature.go\n+++ b/feature.go\n@@ -1 +1 @@\n-package main\n+package main\n"
	return f
}

func TestManaged_Passed(t *testing.T) {
	opts := e2eOptions(t, "G-passed", "M-passed", agent.Findings{Findings: []agent.Finding{}})

	res := runManaged(t, context.Background(), opts)
	if res.exitCode != 0 {
		t.Fatalf("expected exit 0, got %d (stderr: %s)", res.exitCode, res.stderr)
	}
	if got := terminalOutcome(t, res.events); got != "passed" {
		t.Errorf("expected outcome passed, got %q (stderr: %s)", got, res.stderr)
	}
	if res.events[0]["event"] != "run.started" {
		t.Errorf("first event is %q, want run.started", res.events[0]["event"])
	}
	// base_sha must be present on every event.
	for i, ev := range res.events {
		if ev["base_sha"] != opts.BaseSHA {
			t.Errorf("event[%d]: base_sha=%q, want %q", i, ev["base_sha"], opts.BaseSHA)
		}
	}
}

func TestManaged_NeedsDecision(t *testing.T) {
	opts := e2eOptions(t, "G-nd", "M-nd", agent.Findings{Findings: []agent.Finding{
		finding(agent.FindingAskUser, "sec.api-key", "security", "API key in source code", "feature.go"),
	}})

	res := runManaged(t, context.Background(), opts)
	if res.exitCode != 3 {
		t.Fatalf("expected exit 3, got %d (stderr: %s)", res.exitCode, res.stderr)
	}
	if got := terminalOutcome(t, res.events); got != "needs_decision" {
		t.Errorf("expected outcome needs_decision, got %q", got)
	}
	if fp := findingFingerprint(t, res.events); fp == "" {
		t.Error("expected a finding.reported event with a fingerprint")
	}
}

func TestManaged_FailedRetryable(t *testing.T) {
	opts := e2eOptions(t, "G-fr", "M-fr", agent.Findings{Findings: []agent.Finding{
		autoFixFinding("style.gofmt", "style", "needs gofmt", "feature.go"),
	}})

	res := runManaged(t, context.Background(), opts)
	if res.exitCode != 4 {
		t.Fatalf("expected exit 4, got %d (stderr: %s)", res.exitCode, res.stderr)
	}
	if got := terminalOutcome(t, res.events); got != "failed_retryable" {
		t.Errorf("expected outcome failed_retryable, got %q", got)
	}
}

func TestManaged_FailedTerminal(t *testing.T) {
	opts := e2eOptions(t, "G-ft", "M-ft", agent.Findings{Findings: []agent.Finding{
		finding(agent.FindingBlocking, "sec.sqli", "security", "SQL injection", "feature.go"),
	}})

	res := runManaged(t, context.Background(), opts)
	if res.exitCode != 5 {
		t.Fatalf("expected exit 5, got %d (stderr: %s)", res.exitCode, res.stderr)
	}
	if got := terminalOutcome(t, res.events); got != "failed_terminal" {
		t.Errorf("expected outcome failed_terminal, got %q", got)
	}
}

func TestManaged_RerunWithApprovedDecision(t *testing.T) {
	// First run: ask-user finding with no decision → needs_decision.
	opts := e2eOptions(t, "G-rerun", "M-rerun", agent.Findings{Findings: []agent.Finding{
		finding(agent.FindingAskUser, "arch.new-dep", "project-judgment", "new dependency added", "feature.go"),
	}})

	res1 := runManaged(t, context.Background(), opts)
	if res1.exitCode != 3 {
		t.Fatalf("run1: expected exit 3, got %d (stderr: %s)", res1.exitCode, res1.stderr)
	}
	fp := findingFingerprint(t, res1.events)
	if fp == "" {
		t.Fatal("run1: no fingerprint captured")
	}

	// Second run: same inputs, but supply an approving decision.
	// Note: The Decisions file includes invocation_id from run1 for reference,
	// but it doesn't bind the decision - decisions bind only to (run_id, mission_id, input_sha, base_sha, policy_hash).
	decPath := filepath.Join(t.TempDir(), "decisions.json")
	invID := getInvocationID(t, res1.events)
	if invID == "" {
		t.Fatal("run1: no invocation_id captured")
	}
	decContent := map[string]any{
		"schema_version": 1,
		"run_id":         opts.RunID,
		"invocation_id":  invID, // From first run, informational only
		"mission_id":     opts.MissionID,
		"input_sha":      opts.InputSHA,
		"base_sha":       opts.BaseSHA,
		"policy_hash":    opts.PolicyHash,
		"decisions": []map[string]any{
			{
				"decision_id":         "D-1",
				"finding_fingerprint": fp,
				"outcome":             "approved",
				"scope":               "sha_bound",
				"rationale":           "reviewed and accepted",
			},
		},
	}
	decData, _ := json.MarshalIndent(decContent, "", "  ")
	if err := os.WriteFile(decPath, decData, 0o644); err != nil {
		t.Fatal(err)
	}
	opts.DecisionsPath = decPath

	res2 := runManaged(t, context.Background(), opts)
	if res2.exitCode != 0 {
		t.Fatalf("run2: expected exit 0 after approval, got %d (stderr: %s)", res2.exitCode, res2.stderr)
	}
	if got := terminalOutcome(t, res2.events); got != "passed" {
		t.Errorf("run2: expected outcome passed, got %q", got)
	}
}

func TestManaged_EvidenceInvocationUniqueness(t *testing.T) {
	opts := e2eOptions(t, "G-ev-uniq", "M-ev-uniq", agent.Findings{Findings: []agent.Finding{}})

	res1 := runManaged(t, context.Background(), opts)
	if res1.exitCode != 0 {
		t.Fatalf("run1 exit %d (stderr: %s)", res1.exitCode, res1.stderr)
	}
	res2 := runManaged(t, context.Background(), opts)
	if res2.exitCode != 0 {
		t.Fatalf("run2 exit %d (stderr: %s)", res2.exitCode, res2.stderr)
	}

	// Both runs share the same hashed run-id directory but must have distinct
	// invocation subdirectories, each containing a terminal.json.
	var runDir string
	entries, err := os.ReadDir(opts.EvidenceDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected exactly one hashed run dir, got %d", len(entries))
	}
	runDir = filepath.Join(opts.EvidenceDir, entries[0].Name())

	invEntries, err := os.ReadDir(runDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(invEntries) != 2 {
		t.Fatalf("expected 2 invocation dirs, got %d", len(invEntries))
	}
	for _, e := range invEntries {
		terminalPath := filepath.Join(runDir, e.Name(), "terminal.json")
		if _, err := os.Stat(terminalPath); err != nil {
			t.Errorf("missing terminal.json for invocation %s: %v", e.Name(), err)
		}
	}
}

func TestManaged_ReportOnly_NoWorkspaceMutation(t *testing.T) {
	opts := e2eOptions(t, "G-ro", "M-ro", agent.Findings{Findings: []agent.Finding{
		autoFixFinding("style.gofmt", "style", "needs gofmt", "feature.go"),
	}})

	headBefore := gitRevParse(t, opts.Workspace, "HEAD")
	statusBefore := gitStatus(t, opts.Workspace)

	res := runManaged(t, context.Background(), opts)
	if res.exitCode != 4 {
		t.Fatalf("expected exit 4, got %d (stderr: %s)", res.exitCode, res.stderr)
	}

	headAfter := gitRevParse(t, opts.Workspace, "HEAD")
	statusAfter := gitStatus(t, opts.Workspace)
	if headBefore != headAfter {
		t.Errorf("workspace HEAD changed: before %s after %s", headBefore, headAfter)
	}
	if statusBefore != statusAfter {
		t.Errorf("workspace status changed: before %q after %q", statusBefore, statusAfter)
	}
}

func TestManaged_DuplicateFingerprintDetection(t *testing.T) {
	// Create two findings with the same code, class, paths, and description.
	// They must produce identical fingerprints and trigger infrastructure_error.
	dup := finding(agent.FindingAskUser, "review.style", "style", "duplicate finding", "main.go")
	opts := e2eOptions(t, "G-dup", "M-dup", agent.Findings{Findings: []agent.Finding{
		dup,
		dup, // same finding repeated
	}})

	res := runManaged(t, context.Background(), opts)
	if res.exitCode != 1 {
		t.Fatalf("expected exit 1 (infrastructure_error), got %d (stderr: %s)", res.exitCode, res.stderr)
	}
	if got := terminalOutcome(t, res.events); got != "infrastructure_error" {
		t.Errorf("expected outcome infrastructure_error, got %q", got)
	}
	// Verify that the error message mentions duplicate fingerprints.
	terminalMsg := ""
	for _, ev := range res.events {
		if ev["event"] != "run.completed" {
			continue
		}
		payload, ok := ev["payload"].(map[string]any)
		if !ok {
			continue
		}
		if msg, ok := payload["message"].(string); ok {
			terminalMsg = msg
		}
	}
	if !strings.Contains(terminalMsg, "share") || !strings.Contains(terminalMsg, "fingerprint") {
		t.Errorf("error message should mention duplicate fingerprints, got: %q", terminalMsg)
	}
}

func TestManaged_ValidateOptions_UsageError(t *testing.T) {
	// Missing RunID → exit 2, no events emitted.
	opts := &managed.Options{
		MissionID:     "M-1",
		Workspace:     "/tmp/ws",
		BaseSHA:       strings.Repeat("a", 40),
		InputSHA:      strings.Repeat("a", 40),
		TrustedConfig: "/tmp/tc",
		PolicyHash:    "sha256:" + strings.Repeat("a", 64),
		EvidenceDir:   "/tmp/ev",
	}
	res := runManaged(t, context.Background(), opts)
	if res.exitCode != 2 {
		t.Errorf("expected exit 2, got %d", res.exitCode)
	}
	if len(res.events) != 0 {
		t.Errorf("expected no events for usage error, got %d", len(res.events))
	}
}

// findingFingerprint returns the fingerprint of the first finding.reported event.
func findingFingerprint(t *testing.T, events []map[string]any) string {
	t.Helper()
	for _, ev := range events {
		if ev["event"] != "finding.reported" {
			continue
		}
		payload, ok := ev["payload"].(map[string]any)
		if !ok {
			continue
		}
		if fp, ok := payload["fingerprint"].(string); ok {
			return fp
		}
	}
	return ""
}

func getInvocationID(t *testing.T, events []map[string]any) string {
	t.Helper()
	if len(events) == 0 {
		return ""
	}
	// Get invocation_id from first event (all events in a run have the same invocation_id)
	if id, ok := events[0]["invocation_id"].(string); ok {
		return id
	}
	return ""
}

func gitStatus(t *testing.T, dir string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", dir, "status", "--porcelain", "--untracked-files=all").Output()
	if err != nil {
		t.Fatalf("git status: %v", err)
	}
	return strings.TrimSpace(string(out))
}
