package managed_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/douglasjarquin/made/internal/agent"
	"github.com/douglasjarquin/made/internal/agent/agenttest"
	"github.com/douglasjarquin/made/internal/managed"
)

// makeTrustedConfigWithFiles writes trustedConfigRelPath and every entry in
// extraFiles (relative path -> content) under one fresh directory, standing
// in for the trusted base managed mode reads guides from (project issue
// #40). It returns the trusted-config's absolute path and its policy hash.
func makeTrustedConfigWithFiles(t *testing.T, trustedConfigRelPath, content string, extraFiles map[string]string) (path, hash string) {
	t.Helper()
	dir := t.TempDir()
	path = filepath.Join(dir, filepath.FromSlash(trustedConfigRelPath))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir trusted config dir: %v", err)
	}
	writeFile(t, path, content)
	for rel, body := range extraFiles {
		full := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir extra file dir: %v", err)
		}
		writeFile(t, full, body)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	hash = "sha256:" + hex.EncodeToString(sum[:])
	return path, hash
}

func sha256Hex(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

func TestManaged_InternalReview_ReceivesGuideInstructionsAndReadCommand(t *testing.T) {
	guideContent := "# Feature Map\nSelectively follow linked entries.\n"
	configContent := "version: 1\nagent: codex\ncommands:\n  test: \"true\"\n  lint: \"true\"\nreview:\n  guides:\n    - \".made/features/README.md\"\n"
	configPath, policyHash := makeTrustedConfigWithFiles(t, ".made.yml", configContent, map[string]string{
		".made/features/README.md": guideContent,
	})

	workspace, baseSHA, inputSHA := makeTestRepo(t)
	evidenceDir := makeEvidenceDir(t)
	scenario := writeScenario(t, agent.Findings{Findings: []agent.Finding{}})
	bin := agenttest.Build(t)
	logPath := filepath.Join(t.TempDir(), "fakeagent.log")

	opts := &managed.Options{
		RunID:                 "G-guides",
		MissionID:             "M-guides",
		Workspace:             workspace,
		BaseSHA:               baseSHA,
		InputSHA:              inputSHA,
		TrustedConfig:         configPath,
		PolicyHash:            policyHash,
		EvidenceDir:           evidenceDir,
		ReviewAgentBinaryPath: bin,
		ReviewAgentExtraEnv: []string{
			"FAKE_AGENT_SCENARIO=" + scenario,
			"FAKE_AGENT_LOG_FILE=" + logPath,
		},
	}

	res := runManaged(t, context.Background(), opts)
	if res.exitCode != 0 {
		t.Fatalf("expected exit 0, got %d (stderr: %s)", res.exitCode, res.stderr)
	}
	if got := terminalOutcome(t, res.events); got != "passed" {
		t.Fatalf("expected outcome passed, got %q", got)
	}

	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read fakeagent log: %v", err)
	}
	log := string(logData)
	if !strings.Contains(log, agent.ReviewGuideInstruction) {
		t.Errorf("fakeagent task log missing guide instruction: %s", log)
	}
	wantCommand := "git show " + baseSHA + ":.made/features/README.md"
	if !strings.Contains(log, wantCommand) {
		t.Errorf("fakeagent task log missing guide read command %q: %s", wantCommand, log)
	}
	wantHash := "sha256:" + sha256Hex(guideContent)
	if !strings.Contains(log, wantHash) {
		t.Errorf("fakeagent task log missing guide content hash %q: %s", wantHash, log)
	}
}

func TestManaged_NoGuidesConfigured_TaskTextOmitsGuideMention(t *testing.T) {
	configContent := "version: 1\nagent: codex\ncommands:\n  test: \"true\"\n  lint: \"true\"\n"
	configPath, policyHash := makeTrustedConfigWithFiles(t, ".made.yml", configContent, nil)

	workspace, baseSHA, inputSHA := makeTestRepo(t)
	evidenceDir := makeEvidenceDir(t)
	scenario := writeScenario(t, agent.Findings{Findings: []agent.Finding{}})
	bin := agenttest.Build(t)
	logPath := filepath.Join(t.TempDir(), "fakeagent.log")

	opts := &managed.Options{
		RunID:                 "G-noguides",
		MissionID:             "M-noguides",
		Workspace:             workspace,
		BaseSHA:               baseSHA,
		InputSHA:              inputSHA,
		TrustedConfig:         configPath,
		PolicyHash:            policyHash,
		EvidenceDir:           evidenceDir,
		ReviewAgentBinaryPath: bin,
		ReviewAgentExtraEnv: []string{
			"FAKE_AGENT_SCENARIO=" + scenario,
			"FAKE_AGENT_LOG_FILE=" + logPath,
		},
	}

	res := runManaged(t, context.Background(), opts)
	if res.exitCode != 0 {
		t.Fatalf("expected exit 0, got %d (stderr: %s)", res.exitCode, res.stderr)
	}

	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read fakeagent log: %v", err)
	}
	if strings.Contains(strings.ToLower(string(logData)), "guide") {
		t.Errorf("expected no guide mention in task text with no guides configured, got: %s", logData)
	}
}

func TestManaged_MissingConfiguredGuideFailsBeforeReview(t *testing.T) {
	configContent := "version: 1\nagent: codex\ncommands:\n  test: \"true\"\n  lint: \"true\"\nreview:\n  guides:\n    - \"docs/missing.md\"\n"
	configPath, policyHash := makeTrustedConfigWithFiles(t, ".made.yml", configContent, nil)

	workspace, baseSHA, inputSHA := makeTestRepo(t)
	evidenceDir := makeEvidenceDir(t)

	opts := &managed.Options{
		RunID:         "G-missing-guide",
		MissionID:     "M-missing-guide",
		Workspace:     workspace,
		BaseSHA:       baseSHA,
		InputSHA:      inputSHA,
		TrustedConfig: configPath,
		PolicyHash:    policyHash,
		EvidenceDir:   evidenceDir,
	}

	res := runManaged(t, context.Background(), opts)
	if res.exitCode != 1 {
		t.Fatalf("expected exit 1 (infrastructure_error), got %d (stderr: %s)", res.exitCode, res.stderr)
	}
	if got := terminalOutcome(t, res.events); got != "infrastructure_error" {
		t.Errorf("expected outcome infrastructure_error, got %q", got)
	}
	term := findTerminalEvent(t, res.events)
	payload, _ := term["payload"].(map[string]any)
	if stage, _ := payload["stage"].(string); stage != "" {
		t.Errorf("expected no stage to have started before guide resolution fails, got stage=%q", stage)
	}
}

func TestManaged_DuplicateGuidePathRejectedAtConfigLoad(t *testing.T) {
	configContent := "version: 1\nagent: codex\ncommands:\n  test: \"true\"\n  lint: \"true\"\nreview:\n  guides:\n    - \"docs/a.md\"\n    - \"docs/./a.md\"\n"
	configPath, policyHash := makeTrustedConfigWithFiles(t, ".made.yml", configContent, map[string]string{
		"docs/a.md": "content",
	})

	workspace, baseSHA, inputSHA := makeTestRepo(t)
	evidenceDir := makeEvidenceDir(t)

	opts := &managed.Options{
		RunID:         "G-dup-guide",
		MissionID:     "M-dup-guide",
		Workspace:     workspace,
		BaseSHA:       baseSHA,
		InputSHA:      inputSHA,
		TrustedConfig: configPath,
		PolicyHash:    policyHash,
		EvidenceDir:   evidenceDir,
	}

	res := runManaged(t, context.Background(), opts)
	if res.exitCode != 1 {
		t.Fatalf("expected exit 1 (infrastructure_error), got %d (stderr: %s)", res.exitCode, res.stderr)
	}
}

func TestManaged_GuidesWorkWithDirectoryConfigLayout(t *testing.T) {
	guideContent := "index content\n"
	configContent := "version: 1\nagent: codex\ncommands:\n  test: \"true\"\n  lint: \"true\"\nreview:\n  guides:\n    - \".made/features/README.md\"\n"
	configPath, policyHash := makeTrustedConfigWithFiles(t, ".made/config.yaml", configContent, map[string]string{
		".made/features/README.md": guideContent,
	})

	workspace, baseSHA, inputSHA := makeTestRepo(t)
	evidenceDir := makeEvidenceDir(t)
	scenario := writeScenario(t, agent.Findings{Findings: []agent.Finding{}})
	bin := agenttest.Build(t)

	opts := &managed.Options{
		RunID:                 "G-dirlayout",
		MissionID:             "M-dirlayout",
		Workspace:             workspace,
		BaseSHA:               baseSHA,
		InputSHA:              inputSHA,
		TrustedConfig:         configPath,
		PolicyHash:            policyHash,
		EvidenceDir:           evidenceDir,
		ReviewAgentBinaryPath: bin,
		ReviewAgentExtraEnv:   []string{"FAKE_AGENT_SCENARIO=" + scenario},
	}

	res := runManaged(t, context.Background(), opts)
	if res.exitCode != 0 {
		t.Fatalf("expected exit 0, got %d (stderr: %s)", res.exitCode, res.stderr)
	}
	if got := terminalOutcome(t, res.events); got != "passed" {
		t.Errorf("expected outcome passed, got %q", got)
	}
}

func TestManaged_GuideResolvedFromTrustedRootNotCandidateWorkspace(t *testing.T) {
	trustedGuideContent := "TRUSTED CONTENT"
	configContent := "version: 1\nagent: codex\ncommands:\n  test: \"true\"\n  lint: \"true\"\nreview:\n  guides:\n    - \".made/features/README.md\"\n"
	configPath, policyHash := makeTrustedConfigWithFiles(t, ".made.yml", configContent, map[string]string{
		".made/features/README.md": trustedGuideContent,
	})

	workspace, baseSHA, _ := makeTestRepo(t)
	// A candidate-controlled copy at the identical relative path, with
	// different content, in the workspace managed mode actually reviews.
	// Guide resolution must never read this file (project issue #40 trust
	// model: guides come from the trusted base, never the candidate).
	candidatePath := filepath.Join(workspace, ".made", "features", "README.md")
	if err := os.MkdirAll(filepath.Dir(candidatePath), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, candidatePath, "CANDIDATE EDIT")
	for _, args := range [][]string{
		{"add", ".made/features/README.md"},
		{"-c", "commit.gpgsign=false", "commit", "-m", "candidate: add guide-shaped file"},
	} {
		c := exec.Command("git", append([]string{"-C", workspace}, args...)...)
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	inputSHA := gitRevParse(t, workspace, "HEAD")

	evidenceDir := makeEvidenceDir(t)
	scenario := writeScenario(t, agent.Findings{Findings: []agent.Finding{}})
	bin := agenttest.Build(t)
	logPath := filepath.Join(t.TempDir(), "fakeagent.log")

	opts := &managed.Options{
		RunID:                 "G-trust",
		MissionID:             "M-trust",
		Workspace:             workspace,
		BaseSHA:               baseSHA,
		InputSHA:              inputSHA,
		TrustedConfig:         configPath,
		PolicyHash:            policyHash,
		EvidenceDir:           evidenceDir,
		ReviewAgentBinaryPath: bin,
		ReviewAgentExtraEnv: []string{
			"FAKE_AGENT_SCENARIO=" + scenario,
			"FAKE_AGENT_LOG_FILE=" + logPath,
		},
	}

	res := runManaged(t, context.Background(), opts)
	if res.exitCode != 0 {
		t.Fatalf("expected exit 0, got %d (stderr: %s)", res.exitCode, res.stderr)
	}

	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read fakeagent log: %v", err)
	}
	log := string(logData)
	if !strings.Contains(log, "sha256:"+sha256Hex(trustedGuideContent)) {
		t.Errorf("expected the trusted guide's content hash in the task log, got: %s", log)
	}
	if strings.Contains(log, sha256Hex("CANDIDATE EDIT")) {
		t.Errorf("candidate workspace content leaked into the trusted guide binding: %s", log)
	}
}

func TestManaged_MultipleGuidesAllBoundInOrder(t *testing.T) {
	configContent := "version: 1\nagent: codex\ncommands:\n  test: \"true\"\n  lint: \"true\"\n" +
		"review:\n  guides:\n    - \"docs/b.md\"\n    - \"docs/a.md\"\n    - \".made/features/README.md\"\n"
	configPath, policyHash := makeTrustedConfigWithFiles(t, ".made.yml", configContent, map[string]string{
		"docs/b.md":                 "guide b",
		"docs/a.md":                 "guide a",
		".made/features/README.md": "feature index",
	})

	workspace, baseSHA, inputSHA := makeTestRepo(t)
	evidenceDir := makeEvidenceDir(t)
	scenario := writeScenario(t, agent.Findings{Findings: []agent.Finding{}})
	bin := agenttest.Build(t)
	logPath := filepath.Join(t.TempDir(), "fakeagent.log")

	opts := &managed.Options{
		RunID:                 "G-multi-guide",
		MissionID:             "M-multi-guide",
		Workspace:             workspace,
		BaseSHA:               baseSHA,
		InputSHA:              inputSHA,
		TrustedConfig:         configPath,
		PolicyHash:            policyHash,
		EvidenceDir:           evidenceDir,
		ReviewAgentBinaryPath: bin,
		ReviewAgentExtraEnv: []string{
			"FAKE_AGENT_SCENARIO=" + scenario,
			"FAKE_AGENT_LOG_FILE=" + logPath,
		},
	}
	res := runManaged(t, context.Background(), opts)
	if res.exitCode != 0 {
		t.Fatalf("expected exit 0, got %d (stderr: %s)", res.exitCode, res.stderr)
	}

	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read fakeagent log: %v", err)
	}
	log := string(logData)
	// Order matters: b before a before the feature index, matching
	// review.guides exactly (project issue #40 requires guide order to be
	// preserved end to end, not just resolved).
	bIdx := strings.Index(log, "docs/b.md")
	aIdx := strings.Index(log, "docs/a.md")
	readmeIdx := strings.Index(log, ".made/features/README.md")
	if bIdx < 0 || aIdx < 0 || readmeIdx < 0 {
		t.Fatalf("expected all three guides in the task log, got: %s", log)
	}
	if !(bIdx < aIdx && aIdx < readmeIdx) {
		t.Fatalf("expected guide order b, a, README preserved in task log, got indices %d,%d,%d", bIdx, aIdx, readmeIdx)
	}
}

func TestManaged_CandidateDeletesGuideDoesNotAffectTrustedBinding(t *testing.T) {
	trustedGuideContent := "TRUSTED CONTENT SURVIVES DELETION"
	configContent := "version: 1\nagent: codex\ncommands:\n  test: \"true\"\n  lint: \"true\"\nreview:\n  guides:\n    - \".made/features/README.md\"\n"
	configPath, policyHash := makeTrustedConfigWithFiles(t, ".made.yml", configContent, map[string]string{
		".made/features/README.md": trustedGuideContent,
	})

	// The candidate workspace never had this guide path at all, simulating a
	// candidate deletion of a file the trusted base still governs review
	// under (project issue #40: a candidate deletion never removes the
	// trusted guide from the current Review).
	workspace, baseSHA, inputSHA := makeTestRepo(t)
	evidenceDir := makeEvidenceDir(t)
	scenario := writeScenario(t, agent.Findings{Findings: []agent.Finding{}})
	bin := agenttest.Build(t)
	logPath := filepath.Join(t.TempDir(), "fakeagent.log")

	opts := &managed.Options{
		RunID:                 "G-deleted-guide",
		MissionID:             "M-deleted-guide",
		Workspace:             workspace,
		BaseSHA:               baseSHA,
		InputSHA:              inputSHA,
		TrustedConfig:         configPath,
		PolicyHash:            policyHash,
		EvidenceDir:           evidenceDir,
		ReviewAgentBinaryPath: bin,
		ReviewAgentExtraEnv: []string{
			"FAKE_AGENT_SCENARIO=" + scenario,
			"FAKE_AGENT_LOG_FILE=" + logPath,
		},
	}
	res := runManaged(t, context.Background(), opts)
	if res.exitCode != 0 {
		t.Fatalf("expected exit 0 (trusted guide still resolved despite candidate absence), got %d (stderr: %s)", res.exitCode, res.stderr)
	}

	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read fakeagent log: %v", err)
	}
	if !strings.Contains(string(logData), "sha256:"+sha256Hex(trustedGuideContent)) {
		t.Errorf("expected the trusted guide's content hash despite candidate deletion, got: %s", logData)
	}
}

func TestManaged_GuideContentWithShellCommandsNeverExecuted(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "should-never-be-created")
	guideContent := "# Runbook\n\nRun this to reset the database:\n\n```bash\ntouch " + marker + "\nrm -rf /\n```\n"
	configContent := "version: 1\nagent: codex\ncommands:\n  test: \"true\"\n  lint: \"true\"\nreview:\n  guides:\n    - \"docs/runbook.md\"\n"
	configPath, policyHash := makeTrustedConfigWithFiles(t, ".made.yml", configContent, map[string]string{
		"docs/runbook.md": guideContent,
	})

	workspace, baseSHA, inputSHA := makeTestRepo(t)
	evidenceDir := makeEvidenceDir(t)
	scenario := writeScenario(t, agent.Findings{Findings: []agent.Finding{}})
	bin := agenttest.Build(t)

	opts := &managed.Options{
		RunID:                 "G-shell-in-guide",
		MissionID:             "M-shell-in-guide",
		Workspace:             workspace,
		BaseSHA:               baseSHA,
		InputSHA:              inputSHA,
		TrustedConfig:         configPath,
		PolicyHash:            policyHash,
		EvidenceDir:           evidenceDir,
		ReviewAgentBinaryPath: bin,
		ReviewAgentExtraEnv:   []string{"FAKE_AGENT_SCENARIO=" + scenario},
	}
	res := runManaged(t, context.Background(), opts)
	if res.exitCode != 0 {
		t.Fatalf("expected exit 0, got %d (stderr: %s)", res.exitCode, res.stderr)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("guide prose containing a shell command must never be executed")
	}
	if _, err := os.Stat(workspace); err != nil {
		t.Fatalf("workspace must survive an untrusted 'rm -rf /' embedded in guide prose: %v", err)
	}
}

func TestManaged_ExternalReview_GuidesConsultedEchoAccepted(t *testing.T) {
	guideContent := "index\n"
	configContent := "version: 1\nreview:\n  required: true\n  guides:\n    - \".made/features/README.md\"\n"
	configPath, policyHash := makeTrustedConfigWithFiles(t, ".made.yml", configContent, map[string]string{
		".made/features/README.md": guideContent,
	})
	workspace, baseSHA, inputSHA := makeTestRepo(t)
	evidenceDir := makeEvidenceDir(t)

	guideHash := "sha256:" + sha256Hex(guideContent)
	contractHash, err := managed.BuildReviewContract(baseSHA, inputSHA, policyHash, []managed.GuideBinding{
		{Path: ".made/features/README.md", ContentHash: guideHash, Bytes: len(guideContent)},
	}).Hash()
	if err != nil {
		t.Fatal(err)
	}

	resultPath := filepath.Join(t.TempDir(), "review-result.json")
	result := map[string]any{
		"schema_version":          managed.ExternalReviewSchemaVersion,
		"review_contract_version": managed.ReviewContractVersion,
		"executor":                "cursor",
		"reviewer":                "cursor-cloud",
		"requested_model":         "claude-opus-5",
		"actual_model":            nil,
		"base_sha":                baseSHA,
		"input_sha":               inputSHA,
		"policy_hash":             policyHash,
		"review_contract_hash":    contractHash,
		"findings":                []any{},
		"guides_consulted": []map[string]any{
			{"path": ".made/features/README.md", "content_hash": guideHash},
		},
	}
	data, _ := json.Marshal(result)
	if err := os.WriteFile(resultPath, data, 0o644); err != nil {
		t.Fatal(err)
	}

	opts := &managed.Options{
		RunID:         "G-ext-guides",
		MissionID:     "M-ext-guides",
		Workspace:     workspace,
		BaseSHA:       baseSHA,
		InputSHA:      inputSHA,
		TrustedConfig: configPath,
		PolicyHash:    policyHash,
		EvidenceDir:   evidenceDir,
		ReviewSource:  "external",
		ReviewResult:  resultPath,
	}
	res := runManaged(t, context.Background(), opts)
	if res.exitCode != 0 {
		t.Fatalf("expected exit 0, got %d (stderr: %s)", res.exitCode, res.stderr)
	}
	if got := terminalOutcome(t, res.events); got != "passed" {
		t.Errorf("expected outcome passed, got %q", got)
	}
}

func TestManaged_ExternalReview_StaleContractHashRejectedWhenGuidesChange(t *testing.T) {
	guideContent := "index\n"
	configContent := "version: 1\nreview:\n  required: true\n  guides:\n    - \".made/features/README.md\"\n"
	configPath, policyHash := makeTrustedConfigWithFiles(t, ".made.yml", configContent, map[string]string{
		".made/features/README.md": guideContent,
	})
	workspace, baseSHA, inputSHA := makeTestRepo(t)
	evidenceDir := makeEvidenceDir(t)

	// A contract hash computed as if no guides were configured: stale
	// relative to the trusted config's actual guides.
	staleContractHash, err := managed.BuildReviewContract(baseSHA, inputSHA, policyHash, nil).Hash()
	if err != nil {
		t.Fatal(err)
	}

	resultPath := filepath.Join(t.TempDir(), "review-result.json")
	result := map[string]any{
		"schema_version":          managed.ExternalReviewSchemaVersion,
		"review_contract_version": managed.ReviewContractVersion,
		"executor":                "cursor",
		"reviewer":                "cursor-cloud",
		"requested_model":         "claude-opus-5",
		"actual_model":            nil,
		"base_sha":                baseSHA,
		"input_sha":               inputSHA,
		"policy_hash":             policyHash,
		"review_contract_hash":    staleContractHash,
		"findings":                []any{},
	}
	data, _ := json.Marshal(result)
	if err := os.WriteFile(resultPath, data, 0o644); err != nil {
		t.Fatal(err)
	}

	opts := &managed.Options{
		RunID:         "G-ext-stale-guides",
		MissionID:     "M-ext-stale-guides",
		Workspace:     workspace,
		BaseSHA:       baseSHA,
		InputSHA:      inputSHA,
		TrustedConfig: configPath,
		PolicyHash:    policyHash,
		EvidenceDir:   evidenceDir,
		ReviewSource:  "external",
		ReviewResult:  resultPath,
	}
	res := runManaged(t, context.Background(), opts)
	if res.exitCode != 1 {
		t.Fatalf("expected exit 1 (infrastructure_error) for a review-contract hash that doesn't cover the configured guides, got %d (stderr: %s)", res.exitCode, res.stderr)
	}
	if got := terminalOutcome(t, res.events); got != "infrastructure_error" {
		t.Errorf("expected outcome infrastructure_error, got %q", got)
	}
}
