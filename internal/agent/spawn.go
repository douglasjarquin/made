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
	Timeout      time.Duration
}

const defaultSpawnTimeout = 30 * time.Minute

func Spawn(ctx context.Context, kind Kind, params SpawnParams) (Findings, error) {
	binary := params.BinaryPath
	if binary == "" {
		binary = kind.binaryName()
	}

	reviewPath, protectedPaths, maskPaths, cleanupReview, err := prepareReviewWorktree(ctx, params.WorktreePath)
	if err != nil {
		return Findings{}, fmt.Errorf("agent: prepare read-only review worktree: %w", err)
	}
	defer cleanupReview()

	args, cleanup, err := invocation(kind, reviewPath)
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

	findings, err := decodeFindings(result.Stdout)
	if err != nil {
		return Findings{}, fmt.Errorf("agent: parse findings from %s: %w: stdout=%s", kind, err, evidence.RedactString(string(result.Stdout)))
	}
	return findings, nil
}

func reviewEnvironmentForDir(extra []string, dir string) []string {
	filtered := make([]string, 0, len(os.Environ())+len(extra))
	for _, entry := range os.Environ() {
		name, _, ok := strings.Cut(entry, "=")
		if ok && !sensitiveEnvironmentName(name) && !reviewPathEnvironmentName(name) && (dir == "" || name != "PWD") {
			filtered = append(filtered, entry)
		}
	}
	for _, entry := range extra {
		name, _, ok := strings.Cut(entry, "=")
		if ok && !sensitiveEnvironmentName(name) && !reviewPathEnvironmentName(name) && (dir == "" || name != "PWD") {
			filtered = append(filtered, entry)
		}
	}
	if dir != "" {
		filtered = append(filtered, "PWD="+dir)
	}
	filtered = append(filtered, "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	return filtered
}

func reviewPathEnvironmentName(name string) bool {
	return name == "OLDPWD" || strings.HasPrefix(name, "GIT_")
}

func sensitiveEnvironmentName(name string) bool {
	upper := strings.ToUpper(name)
	if upper == "SSH_AUTH_SOCK" || upper == "COOKIE" {
		return true
	}
	for _, marker := range []string{"TOKEN", "SECRET", "PASSWORD", "PASSWD", "API_KEY", "PRIVATE_KEY", "CREDENTIAL"} {
		if strings.Contains(upper, marker) {
			return true
		}
	}
	return false
}

func invocation(kind Kind, worktree string) ([]string, func(), error) {
	if kind != KindCodex {
		return []string{"review", "--worktree", worktree}, func() {}, nil
	}
	dir, err := os.MkdirTemp("", "made-codex-schema-")
	if err != nil {
		return nil, nil, fmt.Errorf("agent: create Codex schema directory: %w", err)
	}
	path := filepath.Join(dir, "output.json")
	if err := os.WriteFile(path, []byte(reviewSchema), 0o600); err != nil {
		_ = os.RemoveAll(dir)
		return nil, nil, fmt.Errorf("agent: write Codex output schema: %w", err)
	}
	return []string{"exec", "--cd", worktree, "--json", "--output-schema", path, "-"}, func() { _ = os.RemoveAll(dir) }, nil
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
