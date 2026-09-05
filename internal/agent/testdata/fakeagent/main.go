// Command fakeagent is a deterministic test double for every reviewer CLI
// Made can spawn (codex, claude, cursor-agent, grok; selected by FAKE_AGENT_KIND):
// it never calls a real model, it just replays a scripted findings payload so
// internal/agent and internal/pipeline/review can be tested without network
// access or API keys. Scenario selection and invocation logging are both
// driven by env vars (mirroring how the real adapters pass agent config
// through the environment) rather than flags, since internal/exec.Command
// already has an Env field but no generic flag-injection point.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

func main() {
	if fleetPath := os.Getenv("FAKE_AGENT_FLEET_CONFIG"); fleetPath != "" {
		runFleetMode(fleetPath)
		return
	}

	kind := os.Getenv("FAKE_AGENT_KIND")
	if kind == "" {
		kind = agentKindCodex
	}
	if err := validateInvocation(kind, os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "fakeagent: invalid invocation: %v\n", err)
		os.Exit(2)
	}
	for _, key := range []string{"MADE_TEST_SECRET", "DATABASE_URL", "COOKIE", "JWT_KEY", "KUBECONFIG", "LC_REVIEW_SECRET"} {
		if os.Getenv(key) != "" {
			fmt.Fprintf(os.Stderr, "fakeagent: sensitive environment %s was exposed\n", key)
			os.Exit(3)
		}
	}

	if logPath := os.Getenv("FAKE_AGENT_LOG_FILE"); logPath != "" {
		logInvocation(logPath)
	}
	touchStateDir(kind)

	scenarioPath := os.Getenv("FAKE_AGENT_SCENARIO")
	if code := os.Getenv("FAKE_AGENT_EXIT_CODE"); code != "" && code != "0" {
		if scenarioPath != "" {
			writeOutputFile(scenarioPath, os.Stdout)
		}
		fmt.Fprintf(os.Stderr, "fakeagent: scripted non-zero exit %s\n", code)
		os.Exit(1)
	}

	if path := os.Getenv("FAKE_AGENT_WRITE_PATH"); path != "" {
		data := []byte(os.Getenv("FAKE_AGENT_WRITE_DATA"))
		if err := os.WriteFile(path, data, 0o600); err != nil {
			fmt.Fprintf(os.Stderr, "fakeagent: write requested path %s: %v\n", path, err)
			os.Exit(1)
		}
	}

	if scenarioPath == "" {
		fmt.Fprintln(os.Stderr, "fakeagent: FAKE_AGENT_SCENARIO env var is required")
		os.Exit(1)
	}

	data, err := os.ReadFile(scenarioPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "fakeagent: read scenario %s: %v\n", scenarioPath, err)
		os.Exit(1)
	}

	if _, err := os.Stdout.Write(envelope(kind, data)); err != nil {
		fmt.Fprintf(os.Stderr, "fakeagent: write structured output: %v\n", err)
		os.Exit(1)
	}
}

const (
	agentKindCodex  = "codex"
	agentKindClaude = "claude"
	agentKindCursor = "cursor"
	agentKindGrok   = "grok"
)

func validateInvocation(kind string, args []string) error {
	switch kind {
	case agentKindCodex:
		return validateCodexInvocation(args)
	case agentKindClaude:
		return validateClaudeInvocation(args)
	case agentKindCursor:
		return validateCursorInvocation(args)
	case agentKindGrok:
		return validateGrokInvocation(args)
	default:
		return fmt.Errorf("unknown FAKE_AGENT_KIND %q", kind)
	}
}

// flagValue returns the value following flag, or "" when absent.
func flagValue(args []string, flag string) string {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == flag {
			return args[i+1]
		}
	}
	return ""
}

func hasFlag(args []string, flag string) bool {
	for _, arg := range args {
		if arg == flag {
			return true
		}
	}
	return false
}

func validateClaudeInvocation(args []string) error {
	for _, want := range []string{"-p", "--strict-mcp-config", "--no-session-persistence"} {
		if !hasFlag(args, want) {
			return fmt.Errorf("claude invocation missing %s: %v", want, args)
		}
	}
	if flagValue(args, "--output-format") != "json" || flagValue(args, "--permission-mode") != "plan" {
		return fmt.Errorf("claude invocation must be json output in plan mode: %v", args)
	}
	if !isFindingsSchema(flagValue(args, "--json-schema")) {
		return fmt.Errorf("claude invocation must pass the findings schema inline: %v", args)
	}
	if hasFlag(args, "Edit") || hasFlag(args, "Write") || flagValue(args, "--tools") == "default" {
		return fmt.Errorf("claude invocation must not enable write tools: %v", args)
	}
	return nil
}

func validateCursorInvocation(args []string) error {
	if !hasFlag(args, "-p") || flagValue(args, "--output-format") != "json" {
		return fmt.Errorf("cursor-agent invocation must print json: %v", args)
	}
	if mode := flagValue(args, "--mode"); mode != "ask" && mode != "plan" {
		return fmt.Errorf("cursor-agent invocation must use a read-only mode, got %q", mode)
	}
	if hasFlag(args, "--force") || hasFlag(args, "--yolo") || hasFlag(args, "-f") {
		return fmt.Errorf("cursor-agent invocation must not force-allow commands: %v", args)
	}
	if !filepath.IsAbs(flagValue(args, "--workspace")) {
		return fmt.Errorf("cursor-agent workspace must be absolute: %v", args)
	}
	return nil
}

func validateGrokInvocation(args []string) error {
	promptPath := flagValue(args, "--prompt-file")
	if !filepath.IsAbs(promptPath) {
		return fmt.Errorf("grok prompt file must be absolute: %v", args)
	}
	if _, err := os.Stat(promptPath); err != nil {
		return fmt.Errorf("grok prompt file unreadable: %w", err)
	}
	if !isFindingsSchema(flagValue(args, "--json-schema")) {
		return fmt.Errorf("grok invocation must pass the findings schema inline: %v", args)
	}
	if flagValue(args, "--permission-mode") != "plan" || !filepath.IsAbs(flagValue(args, "--cwd")) {
		return fmt.Errorf("grok invocation must run plan mode in an absolute cwd: %v", args)
	}
	if hasFlag(args, "--always-approve") {
		return fmt.Errorf("grok invocation must not auto-approve tools: %v", args)
	}
	return nil
}

func isFindingsSchema(schema string) bool {
	var parsed struct {
		Properties map[string]any `json:"properties"`
	}
	if err := json.Unmarshal([]byte(schema), &parsed); err != nil {
		return false
	}
	_, ok := parsed.Properties["findings"]
	return ok
}

// envelope wraps the scripted findings the way each real CLI prints its
// non-interactive result, so tests exercise the same extraction path the
// production adapter uses.
func envelope(kind string, findings []byte) []byte {
	text := string(findings)
	var out any
	switch kind {
	case agentKindClaude:
		out = map[string]any{"type": "result", "subtype": "success", "is_error": false, "result": text, "structured_output": json.RawMessage(findings)}
	case agentKindCursor:
		out = map[string]any{"type": "result", "subtype": "success", "is_error": false, "result": "```json\n" + text + "\n```"}
	case agentKindGrok:
		out = map[string]any{"text": text, "stopReason": "end_turn", "structuredOutput": json.RawMessage(findings)}
	default:
		return findings
	}
	data, err := json.Marshal(out)
	if err != nil {
		fmt.Fprintf(os.Stderr, "fakeagent: marshal %s envelope: %v\n", kind, err)
		os.Exit(1)
	}
	return data
}

func validateCodexInvocation(args []string) error {
	if len(args) != 10 && len(args) != 11 {
		return fmt.Errorf("want 10 or 11 arguments, got %d", len(args))
	}
	if args[0] != "exec" || args[1] != "--cd" || args[3] != "--json" || args[4] != "--output-schema" || args[6] != "--sandbox" || args[7] != "read-only" || args[8] != "--ephemeral" {
		return fmt.Errorf("expected codex exec structured flags, got %v", args)
	}
	if len(args) == 10 && args[9] != "-" {
		return fmt.Errorf("expected stdin prompt sentinel, got %v", args)
	}
	if len(args) == 11 && (args[9] != "--ignore-user-config" || args[10] != "-") {
		return fmt.Errorf("expected Codex user-config override, got %v", args)
	}
	if filepath.IsAbs(args[2]) == false || filepath.IsAbs(args[5]) == false {
		return fmt.Errorf("review and schema paths must be absolute")
	}
	if args[2] == "" {
		return fmt.Errorf("review worktree is required")
	}
	return nil
}

func writeOutputFile(path string, output *os.File) {
	data, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "fakeagent: read scripted output %s: %v\n", path, err)
		os.Exit(1)
	}
	if _, err := output.Write(data); err != nil {
		fmt.Fprintf(os.Stderr, "fakeagent: write scripted output: %v\n", err)
		os.Exit(1)
	}
}

func logInvocation(logPath string) {
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	cwd, _ := os.Getwd()
	fmt.Fprintf(f, "invoked: args=%v cwd=%s\n", os.Args, cwd)
	task, _ := io.ReadAll(io.LimitReader(os.Stdin, 1<<20))
	fmt.Fprintf(f, "task=%s\n", task)
}

// fleetEntry is one binary's scripted behavior in a multi-binary fleet
// (internal/agent/agenttest.BuildFleet), keyed by that binary's invoked name
// so several differently-named fake CLIs sharing one process env can each
// behave independently - a single FAKE_AGENT_* env var, being process-wide,
// cannot express "codex missing, claude present-but-unauthed" in one test.
type fleetEntry struct {
	Kind         string `json:"kind"`
	AuthExitCode int    `json:"auth_exit_code"`
	ExitCode     int    `json:"exit_code"`
	Stderr       string `json:"stderr"`
	ScenarioFile string `json:"scenario_file"`
}

func runFleetMode(fleetPath string) {
	data, err := os.ReadFile(fleetPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "fakeagent: read fleet config %s: %v\n", fleetPath, err)
		os.Exit(1)
	}
	var fleet map[string]fleetEntry
	if err := json.Unmarshal(data, &fleet); err != nil {
		fmt.Fprintf(os.Stderr, "fakeagent: parse fleet config %s: %v\n", fleetPath, err)
		os.Exit(1)
	}
	name := filepath.Base(os.Args[0])
	entry, ok := fleet[name]
	if !ok {
		fmt.Fprintf(os.Stderr, "fakeagent: no fleet entry for binary %q\n", name)
		os.Exit(127)
	}
	args := os.Args[1:]
	if isAuthStatusProbe(entry.Kind, args) {
		os.Exit(entry.AuthExitCode)
	}
	if err := validateInvocation(entry.Kind, args); err != nil {
		fmt.Fprintf(os.Stderr, "fakeagent: invalid invocation: %v\n", err)
		os.Exit(2)
	}
	if entry.ExitCode != 0 {
		fmt.Fprint(os.Stderr, entry.Stderr)
		os.Exit(entry.ExitCode)
	}
	findings, err := os.ReadFile(entry.ScenarioFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "fakeagent: read scenario %s: %v\n", entry.ScenarioFile, err)
		os.Exit(1)
	}
	if _, err := os.Stdout.Write(envelope(entry.Kind, findings)); err != nil {
		fmt.Fprintf(os.Stderr, "fakeagent: write structured output: %v\n", err)
		os.Exit(1)
	}
}

// isAuthStatusProbe matches the two real, verified auth-status invocations
// (codex login status / claude auth status) resolve.go shells out to.
// cursor/grok have no confirmed equivalent, so they never match here.
func isAuthStatusProbe(kind string, args []string) bool {
	switch kind {
	case agentKindCodex:
		return len(args) == 2 && args[0] == "login" && args[1] == "status"
	case agentKindClaude:
		return len(args) == 2 && args[0] == "auth" && args[1] == "status"
	default:
		return false
	}
}

// touchStateDir mimics the real CLIs, which persist session state under HOME
// on every turn. When the directory exists the write must succeed, so a
// containment profile that leaves it read-only fails this double the same way
// it would fail the real binary.
func touchStateDir(kind string) {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	dir := filepath.Join(home, "."+kind)
	if _, err := os.Stat(dir); err != nil {
		return
	}
	marker := filepath.Join(dir, "made-fakeagent-state")
	if err := os.WriteFile(marker, []byte("ok\n"), 0o600); err != nil {
		fmt.Fprintf(os.Stderr, "fakeagent: %s state directory is not writable: %v\n", kind, err)
		os.Exit(4)
	}
	_ = os.Remove(marker)
}
