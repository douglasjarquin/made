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

	args, cleanup, err := invocation(kind, params.WorktreePath)
	if err != nil {
		return Findings{}, err
	}
	defer cleanup()
	timeout := params.Timeout
	if timeout <= 0 {
		timeout = defaultSpawnTimeout
	}
	result, err := exec.Run(ctx, exec.Command{
		Name:    binary,
		Args:    args,
		Dir:     params.WorktreePath,
		Env:     append(os.Environ(), params.ExtraEnv...),
		Stdin:   []byte("Return only the Made review JSON object matching the supplied schema.\n"),
		Timeout: timeout,
	})
	if err != nil {
		return Findings{}, fmt.Errorf("agent: spawn %s (%s): %w", kind, binary, err)
	}
	if result.ExitCode != 0 {
		return Findings{}, fmt.Errorf("agent: %s (%s) exited %d: %s", kind, binary, result.ExitCode, result.Stderr)
	}

	findings, err := decodeFindings(result.Stdout)
	if err != nil {
		return Findings{}, fmt.Errorf("agent: parse findings from %s: %w: stdout=%s", kind, err, result.Stdout)
	}
	return findings, nil
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
		case FindingAskUser, FindingBlocking:
		default:
			return Findings{}, fmt.Errorf("unknown finding kind %q", finding.Kind)
		}
	}
	return findings, nil
}

const reviewSchema = `{"type":"object","additionalProperties":false,"required":["findings"],"properties":{"findings":{"type":"array","items":{"type":"object","additionalProperties":false,"required":["kind","description"],"properties":{"kind":{"type":"string","enum":["auto-fixable","ask-user","blocking"]},"description":{"type":"string"},"patch":{"type":"string"},"paths":{"type":"array","items":{"type":"string"}}}}}}}`
