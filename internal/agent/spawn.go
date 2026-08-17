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

	"github.com/douglasjarquin/made/internal/exec"
)

type SpawnParams struct {
	WorktreePath string
	BinaryPath   string
	ExtraEnv     []string
	Task         string
	Timeout      time.Duration
}

func Spawn(ctx context.Context, kind Kind, params SpawnParams) (Findings, error) {
	if kind != KindCodex {
		return Findings{}, fmt.Errorf("agent: %s structured task contract is unsupported", kind)
	}

	binary := params.BinaryPath
	if binary == "" {
		binary = kind.binaryName()
	}

	artifactDir, err := os.MkdirTemp("", "made-agent-")
	if err != nil {
		return Findings{}, fmt.Errorf("agent: create structured-task artifacts: %w", err)
	}
	defer func() { _ = os.RemoveAll(artifactDir) }()

	schemaPath := filepath.Join(artifactDir, "findings.schema.json")
	if err := writeCodexSchema(schemaPath); err != nil {
		return Findings{}, fmt.Errorf("agent: write Codex output schema: %w", err)
	}
	lastMessagePath := filepath.Join(artifactDir, "findings.json")
	task := strings.TrimSpace(params.Task)
	if task == "" {
		task = "Review the current worktree and return only the structured findings object required by the output schema."
	}

	result, err := exec.Run(ctx, exec.Command{
		Name: binary,
		Args: []string{
			"exec",
			"--json",
			"--output-schema", schemaPath,
			"--output-last-message", lastMessagePath,
			"--sandbox", "read-only",
			"--ephemeral",
			"-C", params.WorktreePath,
			task,
		},
		Dir:     params.WorktreePath,
		Env:     reviewEnvironment(params.ExtraEnv),
		Timeout: params.Timeout,
	})
	if err != nil {
		return Findings{}, fmt.Errorf("agent: spawn %s (%s): %w", kind, binary, err)
	}
	if result.ExitCode != 0 {
		return Findings{}, fmt.Errorf("agent: %s (%s) exited %d: %s", kind, binary, result.ExitCode, result.Stderr)
	}

	data, err := os.ReadFile(lastMessagePath)
	if err != nil {
		return Findings{}, fmt.Errorf("agent: read structured output from %s: %w", kind, err)
	}
	findings, err := parseFindings(data)
	if err != nil {
		return Findings{}, fmt.Errorf("agent: parse structured findings from %s: %w", kind, err)
	}
	return findings, nil
}

func reviewEnvironment(extra []string) []string {
	entries := append(os.Environ(), extra...)
	env := make([]string, 0, len(entries))
	for _, entry := range entries {
		key, _, ok := strings.Cut(entry, "=")
		if !ok || !reviewEnvironmentKey(key) {
			continue
		}
		env = append(env, entry)
	}
	return env
}

func reviewEnvironmentKey(key string) bool {
	switch key {
	case "PATH", "HOME", "TMPDIR", "LANG", "TERM", "USER", "LOGNAME", "SHELL", "PWD", "OLDPWD", "NO_COLOR", "CI",
		"FAKE_AGENT_KIND", "FAKE_AGENT_SCENARIO", "FAKE_AGENT_LOG_FILE", "FAKE_AGENT_EXIT_CODE":
		return true
	}
	return strings.HasPrefix(key, "LC_")
}

func writeCodexSchema(path string) error {
	const schema = `{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "type": "object",
  "additionalProperties": false,
  "required": ["findings"],
  "properties": {
    "findings": {
      "type": "array",
      "items": {
        "type": "object",
        "additionalProperties": false,
        "required": ["kind", "description"],
        "properties": {
          "kind": {"type": "string", "enum": ["auto-fixable", "ask-user", "blocking"]},
          "description": {"type": "string"},
          "patch": {"type": "string"}
        }
      }
    }
  }
}`
	return os.WriteFile(path, []byte(schema), 0o600)
}

func parseFindings(data []byte) (Findings, error) {
	var raw map[string]json.RawMessage
	if err := decodeJSON(data, &raw); err != nil {
		return Findings{}, err
	}
	if len(raw) != 1 {
		return Findings{}, fmt.Errorf("object must contain only findings")
	}
	findingsData, ok := raw["findings"]
	if !ok || string(findingsData) == "null" {
		return Findings{}, fmt.Errorf("missing required findings array")
	}

	var findings []Finding
	if err := decodeJSON(findingsData, &findings); err != nil {
		return Findings{}, err
	}
	if findings == nil {
		return Findings{}, fmt.Errorf("findings must be an array")
	}
	return Findings{Findings: findings}, nil
}

func decodeJSON(data []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return fmt.Errorf("trailing JSON value")
		}
		return fmt.Errorf("trailing JSON: %w", err)
	}
	return nil
}
