package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/douglasjarquin/made/internal/evidence"
	"github.com/douglasjarquin/made/internal/exec"
)

type SpawnParams struct {
	WorktreePath   string
	BinaryPath     string
	ExtraEnv       []string
	Task           string
	TrustedBaseSHA string
	Timeout        time.Duration
}

const (
	defaultSpawnTimeout = 30 * time.Minute
	spawnOutputLimit    = 1 << 20
)

type SpawnResult struct {
	Findings Findings
	Task     string
	Response []byte
}

func Spawn(ctx context.Context, kind Kind, params SpawnParams) (Findings, error) {
	result, err := SpawnWithEvidence(ctx, kind, params)
	if err != nil {
		return Findings{}, err
	}
	return result.Findings, nil
}

func SpawnWithEvidence(ctx context.Context, kind Kind, params SpawnParams) (SpawnResult, error) {
	if kind != KindCodex {
		return SpawnResult{}, fmt.Errorf("agent: %s structured task contract is unsupported; supported agents: %s", kind, KindCodex)
	}
	binary := params.BinaryPath
	if binary == "" {
		binary = kind.binaryName()
	}

	reviewPath, protectedPaths, maskPaths, cleanupReview, err := prepareReviewWorktree(ctx, params.WorktreePath, params.TrustedBaseSHA)
	if err != nil {
		return SpawnResult{}, fmt.Errorf("agent: prepare read-only review worktree: %w", err)
	}
	defer cleanupReview()

	args, cleanup, err := invocation(kind, reviewPath)
	if err != nil {
		return SpawnResult{}, err
	}
	defer cleanup()
	commandName, commandArgs, err := containedInvocation(binary, args, reviewPath, protectedPaths, maskPaths)
	if err != nil {
		return SpawnResult{}, fmt.Errorf("agent: contain review process: %w", err)
	}
	task := strings.TrimSpace(params.Task)
	if task == "" {
		task = "Inspect the candidate diff in the detached review repository before deciding that findings are empty. Return only the structured object matching the supplied output schema."
	}
	if len([]byte(task)) > maxReviewTaskBytes {
		return SpawnResult{}, fmt.Errorf("agent: review task exceeds %d bytes", maxReviewTaskBytes)
	}
	timeout := params.Timeout
	if timeout <= 0 || timeout > defaultSpawnTimeout {
		timeout = defaultSpawnTimeout
	}
	result, err := exec.Run(ctx, exec.Command{
		Name:        commandName,
		Args:        commandArgs,
		Dir:         reviewPath,
		Env:         reviewEnvironmentForDir(params.ExtraEnv, reviewPath),
		Stdin:       []byte(task),
		Timeout:     timeout,
		OutputLimit: spawnOutputLimit,
	})
	if err != nil {
		return SpawnResult{}, fmt.Errorf("agent: spawn %s (%s): %w", kind, binary, err)
	}
	if result.ExitCode != 0 {
		return SpawnResult{}, fmt.Errorf("agent: %s (%s) exited %d: %s", kind, binary, result.ExitCode, evidence.RedactString(string(result.Stderr)))
	}

	response, err := extractStructuredResponse(result.Stdout)
	if err != nil {
		return SpawnResult{}, fmt.Errorf("agent: extract structured response from %s: %w: stdout=%s", kind, err, evidence.RedactString(string(result.Stdout)))
	}
	findings, err := strictFindings(response)
	if err != nil {
		return SpawnResult{}, fmt.Errorf("agent: parse findings from %s: %w: stdout=%s", kind, err, evidence.RedactString(string(result.Stdout)))
	}
	return SpawnResult{Findings: findings, Task: task, Response: response}, nil
}

func reviewEnvironmentForDir(extra []string, dir string) []string {
	entries := append(append([]string(nil), os.Environ()...), extra...)
	filtered := make([]string, 0, len(entries)+5)
	for _, entry := range entries {
		name, _, ok := strings.Cut(entry, "=")
		if !ok || !reviewEnvironmentKey(name) || (dir != "" && name == "PWD") {
			continue
		}
		filtered = append(filtered, entry)
	}
	if dir != "" {
		filtered = append(filtered, "PWD="+dir)
	}
	filtered = append(filtered, "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null", "GIT_CONFIG_NOSYSTEM=1", "GIT_TERMINAL_PROMPT=0")
	return filtered
}

func reviewEnvironmentKey(name string) bool {
	switch name {
	case "PATH", "HOME", "TMPDIR", "LANG", "TERM", "USER", "LOGNAME", "SHELL", "PWD", "OLDPWD", "NO_COLOR", "CI",
		"FAKE_AGENT_KIND", "FAKE_AGENT_SCENARIO", "FAKE_AGENT_LOG_FILE", "FAKE_AGENT_EXIT_CODE", "FAKE_AGENT_WRITE_PATH", "FAKE_AGENT_WRITE_DATA", "FAKE_AGENT_BASE_SHA":
		return true
	}
	return strings.HasPrefix(name, "LC_")
}

func invocation(kind Kind, worktree string) ([]string, func(), error) {
	if kind != KindCodex {
		return nil, nil, fmt.Errorf("agent: %s structured task contract is unsupported; supported agents: %s", kind, KindCodex)
	}
	dir, err := os.MkdirTemp("", "made-codex-schema-")
	if err != nil {
		return nil, nil, fmt.Errorf("agent: create Codex schema directory: %w", err)
	}
	schemaPath := filepath.Join(dir, "findings.schema.json")
	if err := os.WriteFile(schemaPath, []byte(reviewSchema), 0o600); err != nil {
		_ = os.RemoveAll(dir)
		return nil, nil, fmt.Errorf("agent: write Codex output schema: %w", err)
	}
	return []string{"exec", "--cd", worktree, "--json", "--output-schema", schemaPath, "-"}, func() { _ = os.RemoveAll(dir) }, nil
}

func extractStructuredResponse(data []byte) ([]byte, error) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return nil, fmt.Errorf("structured output is empty")
	}
	var direct map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &direct); err == nil {
		if _, ok := direct["findings"]; ok {
			return append([]byte(nil), trimmed...), nil
		}
	}

	var response []byte
	for _, line := range bytes.Split(trimmed, []byte{'\n'}) {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		var event struct {
			Type string `json:"type"`
			Item struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"item"`
		}
		if err := json.Unmarshal(line, &event); err != nil {
			return nil, fmt.Errorf("structured output event is invalid: %w", err)
		}
		switch event.Type {
		case "error", "turn.failed":
			return nil, fmt.Errorf("codex returned a failed event")
		case "item.completed":
			if event.Item.Type == "agent_message" && strings.TrimSpace(event.Item.Text) != "" {
				response = []byte(event.Item.Text)
			}
		}
	}
	if len(response) == 0 {
		return nil, fmt.Errorf("structured output event stream did not contain a completed agent message")
	}
	return response, nil
}

func strictFindings(data []byte) (Findings, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var findings Findings
	if err := decoder.Decode(&findings); err != nil {
		return Findings{}, err
	}
	var extra json.RawMessage
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return Findings{}, fmt.Errorf("structured output contains multiple JSON values")
		}
		return Findings{}, err
	}
	for _, finding := range findings.Findings {
		if strings.TrimSpace(finding.Description) == "" {
			return Findings{}, fmt.Errorf("finding description is required")
		}
		switch finding.Kind {
		case FindingAutoFixable:
			if strings.TrimSpace(finding.Patch) == "" {
				return Findings{}, fmt.Errorf("auto-fixable finding patch is required")
			}
			if len(finding.Paths) == 0 {
				return Findings{}, fmt.Errorf("auto-fixable finding paths are required")
			}
		case FindingAskUser, FindingBlocking:
			if strings.TrimSpace(finding.Patch) != "" {
				return Findings{}, fmt.Errorf("%s finding must not include a patch", finding.Kind)
			}
		default:
			return Findings{}, fmt.Errorf("unknown finding kind %q", finding.Kind)
		}
	}
	return findings, nil
}

const reviewSchema = `{"type":"object","additionalProperties":false,"required":["findings"],"properties":{"findings":{"type":"array","items":{"type":"object","additionalProperties":false,"required":["kind","description","patch","paths"],"properties":{"kind":{"type":"string","enum":["auto-fixable","ask-user","blocking"]},"description":{"type":"string"},"patch":{"type":["string","null"]},"paths":{"type":["array","null"],"items":{"type":"string"}}}}}}}`
