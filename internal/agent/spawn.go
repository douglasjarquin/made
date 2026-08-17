package agent

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/douglasjarquin/made/internal/evidence"
	"github.com/douglasjarquin/made/internal/exec"
)

type SpawnParams struct {
	WorktreePath string
	BinaryPath   string
	ExtraEnv     []string
	Task         string
	Timeout      time.Duration
}

const defaultSpawnTimeout = 30 * time.Minute

func Spawn(ctx context.Context, kind Kind, params SpawnParams) (Findings, error) {
	if kind != KindCodex {
		return Findings{}, fmt.Errorf("agent: %s structured task contract is unsupported", kind)
	}
	binary := params.BinaryPath
	if binary == "" {
		binary = kind.binaryName()
	}

	reviewPath, protectedPaths, maskPaths, cleanupReview, err := prepareReviewWorktree(ctx, params.WorktreePath)
	if err != nil {
		return Findings{}, fmt.Errorf("agent: prepare read-only review worktree: %w", err)
	}
	defer cleanupReview()

	args, cleanup, outputPath, err := invocation(kind, reviewPath, params.Task)
	if err != nil {
		return Findings{}, err
	}
	defer cleanup()
	commandName, commandArgs, err := containedInvocation(binary, args, reviewPath, protectedPaths, maskPaths)
	if err != nil {
		return Findings{}, fmt.Errorf("agent: contain review process: %w", err)
	}
	timeout := params.Timeout
	if timeout <= 0 {
		timeout = defaultSpawnTimeout
	}
	result, err := exec.Run(ctx, exec.Command{
		Name:    commandName,
		Args:    commandArgs,
		Dir:     reviewPath,
		Env:     reviewEnvironmentForDir(params.ExtraEnv, reviewPath),
		Stdin:   []byte("Return only the Made review JSON object matching the supplied schema.\n"),
		Timeout: timeout,
	})
	if err != nil {
		return Findings{}, fmt.Errorf("agent: spawn %s (%s): %w", kind, binary, err)
	}
	if result.ExitCode != 0 {
		return Findings{}, fmt.Errorf("agent: %s (%s) exited %d: %s", kind, binary, result.ExitCode, evidence.RedactString(string(result.Stderr)))
	}

	data := result.Stdout
	if outputPath != "" {
		data, err = os.ReadFile(outputPath)
		if err != nil {
			return Findings{}, fmt.Errorf("agent: read structured output from %s: %w", kind, err)
		}
	}
	findings, err := strictFindings(data)
	if err != nil {
		return Findings{}, fmt.Errorf("agent: parse findings from %s: %w: stdout=%s", kind, err, evidence.RedactString(string(result.Stdout)))
	}
	return findings, nil
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
		"FAKE_AGENT_KIND", "FAKE_AGENT_SCENARIO", "FAKE_AGENT_LOG_FILE", "FAKE_AGENT_EXIT_CODE", "FAKE_AGENT_WRITE_PATH", "FAKE_AGENT_WRITE_DATA":
		return true
	}
	return strings.HasPrefix(name, "LC_")
}

func invocation(kind Kind, worktree, task string) ([]string, func(), string, error) {
	if kind != KindCodex {
		return nil, nil, "", fmt.Errorf("agent: %s structured task contract is unsupported", kind)
	}
	dir, err := os.MkdirTemp("", "made-codex-schema-")
	if err != nil {
		return nil, nil, "", fmt.Errorf("agent: create Codex schema directory: %w", err)
	}
	schemaPath := filepath.Join(dir, "findings.schema.json")
	if err := os.WriteFile(schemaPath, []byte(reviewSchema), 0o600); err != nil {
		_ = os.RemoveAll(dir)
		return nil, nil, "", fmt.Errorf("agent: write Codex output schema: %w", err)
	}
	outputPath := filepath.Join(dir, "findings.json")
	if strings.TrimSpace(task) == "" {
		task = "Review the current worktree and return only the structured findings object required by the output schema."
	}
	return []string{"exec", "--json", "--output-schema", schemaPath, "--output-last-message", outputPath, "--sandbox", "read-only", "--ephemeral", "-C", worktree, task}, func() { _ = os.RemoveAll(dir) }, outputPath, nil
}

func decodeFindings(data []byte) (Findings, error) {
	var direct Findings
	if err := json.Unmarshal(data, &direct); err == nil {
		var envelope map[string]json.RawMessage
		if json.Unmarshal(data, &envelope) == nil {
			if raw, ok := envelope["findings"]; ok {
				if string(raw) == "null" {
					return Findings{Findings: []Finding{}}, nil
				}
				if values, err := strictFindings(data); err == nil {
					return values, nil
				}
			}
		}
	}
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 4096), 4*1024*1024)
	var last string
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "{") {
			var event struct {
				Item struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"item"`
			}
			if json.Unmarshal([]byte(line), &event) == nil && event.Item.Type == "agent_message" {
				last = event.Item.Text
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return Findings{}, err
	}
	if last == "" {
		return Findings{}, fmt.Errorf("structured findings payload was not found")
	}
	return strictFindings([]byte(last))
}

func strictFindings(data []byte) (Findings, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var findings Findings
	if err := decoder.Decode(&findings); err != nil {
		return Findings{}, err
	}
	for _, finding := range findings.Findings {
		if finding.Description == "" {
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
		default:
			return Findings{}, fmt.Errorf("unknown finding kind %q", finding.Kind)
		}
	}
	return findings, nil
}

const reviewSchema = `{"type":"object","additionalProperties":false,"required":["findings"],"properties":{"findings":{"type":"array","items":{"type":"object","additionalProperties":false,"required":["kind","description"],"properties":{"kind":{"type":"string","enum":["auto-fixable","ask-user","blocking"]},"description":{"type":"string"},"patch":{"type":"string"},"paths":{"type":"array","items":{"type":"string"}}}}}}}`
