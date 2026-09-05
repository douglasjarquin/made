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
	if _, err := ParseKind(string(kind)); err != nil {
		return SpawnResult{}, fmt.Errorf("agent: %w", err)
	}
	binary := params.BinaryPath
	if binary == "" {
		binary = kind.BinaryName()
	}

	reviewPath, protectedPaths, maskPaths, cleanupReview, err := prepareReviewWorktree(ctx, params.WorktreePath, params.TrustedBaseSHA)
	if err != nil {
		return SpawnResult{}, fmt.Errorf("agent: prepare read-only review worktree: %w", err)
	}
	defer cleanupReview()

	task := strings.TrimSpace(params.Task)
	if task == "" {
		task = "Inspect the candidate diff in the detached review repository before deciding that findings are empty. Return only the structured object matching the supplied output schema."
	}
	if len([]byte(task)) > maxReviewTaskBytes {
		return SpawnResult{}, fmt.Errorf("agent: review task exceeds %d bytes", maxReviewTaskBytes)
	}
	args, stdin, cleanup, err := invocation(kind, reviewPath, task)
	if err != nil {
		return SpawnResult{}, err
	}
	defer cleanup()
	commandName, commandArgs, err := containedInvocation(binary, args, reviewPath, protectedPaths, maskPaths, existingStatePaths(kind))
	if err != nil {
		return SpawnResult{}, fmt.Errorf("agent: contain review process: %w", err)
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
		Stdin:       stdin,
		Timeout:     timeout,
		OutputLimit: spawnOutputLimit,
	})
	if err != nil {
		return SpawnResult{}, fmt.Errorf("agent: spawn %s (%s): %w", kind, binary, err)
	}
	if result.ExitCode != 0 {
		return SpawnResult{}, fmt.Errorf("agent: %s (%s) exited %d: stdout=%s stderr=%s", kind, binary, result.ExitCode, evidence.RedactString(string(result.Stdout)), evidence.RedactString(string(result.Stderr)))
	}

	response, err := extractStructuredResponse(kind, result.Stdout)
	if err != nil {
		return SpawnResult{}, fmt.Errorf("agent: extract structured response from %s: %w: stdout=%s", kind, err, evidence.RedactString(string(result.Stdout)))
	}
	findings, err := strictFindings(response)
	if err != nil {
		return SpawnResult{}, fmt.Errorf("agent: parse findings from %s: %w: stdout=%s", kind, err, evidence.RedactString(string(result.Stdout)))
	}
	return SpawnResult{Findings: findings, Task: task, Response: response}, nil
}

// existingStatePaths resolves kind.stateDirs against HOME, keeping only the
// entries that exist: bubblewrap refuses to bind a missing source.
func existingStatePaths(kind Kind) []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	var paths []string
	for _, rel := range kind.stateDirs() {
		path := filepath.Join(home, rel)
		if _, err := os.Stat(path); err == nil {
			paths = append(paths, path)
		}
	}
	return paths
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
	return false
}

// invocation returns the harness-specific argv for a report-only review, the
// bytes to feed on stdin, and a cleanup for any temp files it wrote. Every
// harness runs in its own read-only mode inside the outer containment from
// containment.go; the task text is delivered the way each CLI reads a
// non-interactive prompt without argv length limits (stdin or a prompt file).
func invocation(kind Kind, worktree, task string) (args []string, stdin []byte, cleanup func(), err error) {
	dir, err := os.MkdirTemp("", "made-review-"+string(kind)+"-")
	if err != nil {
		return nil, nil, nil, fmt.Errorf("agent: create %s invocation directory: %w", kind, err)
	}
	cleanup = func() { _ = os.RemoveAll(dir) }
	fail := func(err error) ([]string, []byte, func(), error) {
		cleanup()
		return nil, nil, nil, err
	}
	switch kind {
	case KindCodex:
		schemaPath := filepath.Join(dir, "findings.schema.json")
		if err := os.WriteFile(schemaPath, []byte(reviewSchema), 0o600); err != nil {
			return fail(fmt.Errorf("agent: write Codex output schema: %w", err))
		}
		return []string{"exec", "--cd", worktree, "--json", "--output-schema", schemaPath, "--sandbox", "read-only", "--ephemeral", "--ignore-user-config", "-"}, []byte(task), cleanup, nil
	case KindClaude:
		// Plan mode keeps every edit tool refused; the explicit tool list keeps
		// the reviewer to reading the worktree; user/project settings and MCP
		// servers stay out so the review is reproducible.
		return []string{"-p", "--output-format", "json", "--json-schema", reviewSchema, "--permission-mode", "plan", "--tools", "Read,Grep,Glob,Bash", "--setting-sources", "", "--strict-mcp-config", "--no-session-persistence"}, []byte(task), cleanup, nil
	case KindCursor:
		// cursor-agent has no output-schema flag, so the schema rides in the
		// task text and strictFindings enforces it on the way back.
		return []string{"-p", "--output-format", "json", "--mode", "ask", "--trust", "--workspace", worktree}, []byte(task + "\n\n" + cursorSchemaInstruction), cleanup, nil
	case KindGrok:
		promptPath := filepath.Join(dir, "task.md")
		if err := os.WriteFile(promptPath, []byte(task), 0o600); err != nil {
			return fail(fmt.Errorf("agent: write Grok prompt file: %w", err))
		}
		return []string{"--prompt-file", promptPath, "--json-schema", reviewSchema, "--permission-mode", "plan", "--cwd", worktree, "--no-subagents", "--disable-web-search", "--verbatim"}, nil, cleanup, nil
	default:
		return fail(fmt.Errorf("agent: unsupported agent %q; supported agents: %s", kind, SupportedKindNames()))
	}
}

const cursorSchemaInstruction = "Respond with only one JSON object and no prose or code fences. It must match this JSON Schema exactly:\n" + reviewSchema

// extractStructuredResponse pulls the findings JSON object out of each
// harness's non-interactive output envelope.
func extractStructuredResponse(kind Kind, data []byte) ([]byte, error) {
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
	switch kind {
	case KindCodex:
		return extractCodexResponse(trimmed)
	case KindClaude:
		return extractClaudeResponse(trimmed)
	case KindCursor:
		return extractCursorResponse(trimmed)
	case KindGrok:
		return extractGrokResponse(trimmed)
	default:
		return nil, fmt.Errorf("unsupported agent %q", kind)
	}
}

func extractCodexResponse(trimmed []byte) ([]byte, error) {
	var response []byte
	for line := range bytes.SplitSeq(trimmed, []byte{'\n'}) {
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

// extractClaudeResponse reads `claude -p --output-format json`: one result
// object whose structured_output carries the schema-constrained object.
func extractClaudeResponse(trimmed []byte) ([]byte, error) {
	var envelope struct {
		Type             string          `json:"type"`
		IsError          bool            `json:"is_error"`
		Result           string          `json:"result"`
		StructuredOutput json.RawMessage `json:"structured_output"`
	}
	if err := json.Unmarshal(trimmed, &envelope); err != nil {
		return nil, fmt.Errorf("claude result envelope is invalid: %w", err)
	}
	if envelope.Type != "result" {
		return nil, fmt.Errorf("claude output is not a result envelope")
	}
	if envelope.IsError {
		return nil, fmt.Errorf("claude returned an error result: %s", strings.TrimSpace(envelope.Result))
	}
	if len(bytes.TrimSpace(envelope.StructuredOutput)) > 0 && !bytes.Equal(bytes.TrimSpace(envelope.StructuredOutput), []byte("null")) {
		return envelope.StructuredOutput, nil
	}
	return stripCodeFence(envelope.Result), nil
}

// extractCursorResponse reads `cursor-agent -p --output-format json`: one
// result object whose result string is the agent's final message.
func extractCursorResponse(trimmed []byte) ([]byte, error) {
	var envelope struct {
		Type    string `json:"type"`
		IsError bool   `json:"is_error"`
		Result  string `json:"result"`
	}
	if err := json.Unmarshal(trimmed, &envelope); err != nil {
		return nil, fmt.Errorf("cursor-agent result envelope is invalid: %w", err)
	}
	if envelope.Type != "result" {
		return nil, fmt.Errorf("cursor-agent output is not a result envelope")
	}
	if envelope.IsError {
		return nil, fmt.Errorf("cursor-agent returned an error result: %s", strings.TrimSpace(envelope.Result))
	}
	// cursor-agent concatenates every assistant text message into result, so
	// narration such as "Inspecting the commit..." can precede the object.
	response, ok := trailingJSONObject(string(stripCodeFence(envelope.Result)))
	if !ok {
		return nil, fmt.Errorf("cursor-agent result did not end with a JSON object")
	}
	return response, nil
}

// trailingJSONObject returns the last complete JSON object in text, which must
// also end the text (ignoring whitespace and a closing code fence).
func trailingJSONObject(text string) ([]byte, bool) {
	text = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(text), "```"))
	if !strings.HasSuffix(text, "}") {
		return nil, false
	}
	for start := strings.LastIndexByte(text, '{'); start >= 0; start = strings.LastIndexByte(text[:start], '{') {
		candidate := text[start:]
		if json.Valid([]byte(candidate)) {
			return []byte(candidate), true
		}
	}
	return nil, false
}

// extractGrokResponse reads `grok --json-schema`: one object whose
// structuredOutput carries the schema-constrained object.
func extractGrokResponse(trimmed []byte) ([]byte, error) {
	var envelope struct {
		Text             string          `json:"text"`
		StopReason       string          `json:"stopReason"`
		StructuredOutput json.RawMessage `json:"structuredOutput"`
	}
	if err := json.Unmarshal(trimmed, &envelope); err != nil {
		return nil, fmt.Errorf("grok result envelope is invalid: %w", err)
	}
	if len(bytes.TrimSpace(envelope.StructuredOutput)) > 0 && !bytes.Equal(bytes.TrimSpace(envelope.StructuredOutput), []byte("null")) {
		return envelope.StructuredOutput, nil
	}
	if strings.TrimSpace(envelope.Text) == "" {
		return nil, fmt.Errorf("grok output did not contain structured output (stop reason %q)", envelope.StopReason)
	}
	return stripCodeFence(envelope.Text), nil
}

// stripCodeFence tolerates a final message wrapped in ```json fences, which
// harnesses without a schema flag sometimes add despite instructions.
func stripCodeFence(text string) []byte {
	s := strings.TrimSpace(text)
	if strings.HasPrefix(s, "```") {
		s = strings.TrimPrefix(s, "```")
		if i := strings.IndexByte(s, '\n'); i >= 0 {
			s = s[i+1:]
		}
		s = strings.TrimSuffix(strings.TrimSpace(s), "```")
	}
	return []byte(strings.TrimSpace(s))
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

const reviewSchema = `{"type":"object","additionalProperties":false,"required":["findings"],"properties":{"findings":{"type":"array","items":{"type":"object","additionalProperties":false,"required":["kind","description","patch","paths","code","class","symbol"],"properties":{"kind":{"type":"string","enum":["auto-fixable","ask-user","blocking"]},"description":{"type":"string"},"patch":{"type":["string","null"]},"paths":{"type":["array","null"],"items":{"type":"string"}},"code":{"type":["string","null"]},"class":{"type":["string","null"]},"symbol":{"type":["string","null"]}}}}}}`
