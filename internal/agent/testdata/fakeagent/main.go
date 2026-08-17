// Command fakeagent is a deterministic test double for the Claude/Codex CLIs:
// it never calls a real model, it just replays a scripted findings payload so
// internal/agent and internal/pipeline/review can be tested without network
// access or API keys. Scenario selection and invocation logging are both
// driven by env vars (mirroring how the real adapters pass agent config
// through the environment) rather than flags, since internal/exec.Command
// already has an Env field but no generic flag-injection point.
package main

import (
	"fmt"
	"os"
	"path/filepath"
)

func main() {
	if os.Getenv("FAKE_AGENT_KIND") != string(agentKindCodex) {
		fmt.Fprintln(os.Stderr, "fakeagent: only the codex structured exec contract is supported")
		os.Exit(2)
	}
	if err := validateInvocation(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "fakeagent: invalid invocation: %v\n", err)
		os.Exit(2)
	}

	if logPath := os.Getenv("FAKE_AGENT_LOG_FILE"); logPath != "" {
		logInvocation(logPath)
	}

	if code := os.Getenv("FAKE_AGENT_EXIT_CODE"); code != "" && code != "0" {
		fmt.Fprintf(os.Stderr, "fakeagent: scripted non-zero exit %s\n", code)
		os.Exit(1)
	}

	scenarioPath := os.Getenv("FAKE_AGENT_SCENARIO")
	if scenarioPath == "" {
		fmt.Fprintln(os.Stderr, "fakeagent: FAKE_AGENT_SCENARIO env var is required")
		os.Exit(1)
	}

	data, err := os.ReadFile(scenarioPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "fakeagent: read scenario %s: %v\n", scenarioPath, err)
		os.Exit(1)
	}

	args := os.Args[1:]
	lastMessagePath := args[5]
	if err := os.WriteFile(lastMessagePath, data, 0o600); err != nil {
		fmt.Fprintf(os.Stderr, "fakeagent: write structured output %s: %v\n", lastMessagePath, err)
		os.Exit(1)
	}
	_, _ = fmt.Fprintln(os.Stdout, `{"type":"turn.completed"}`)
}

const agentKindCodex = "codex"

func validateInvocation(args []string) error {
	if len(args) != 12 {
		return fmt.Errorf("want 12 arguments, got %d", len(args))
	}
	if args[0] != "exec" || args[1] != "--json" || args[2] != "--output-schema" || args[4] != "--output-last-message" || args[6] != "--sandbox" || args[7] != "read-only" || args[8] != "--ephemeral" || args[9] != "-C" {
		return fmt.Errorf("expected codex exec structured flags, got %v", args)
	}
	if filepath.IsAbs(args[3]) == false || filepath.IsAbs(args[5]) == false {
		return fmt.Errorf("schema and output paths must be absolute")
	}
	if args[10] == "" || args[11] == "" {
		return fmt.Errorf("worktree and task are required")
	}
	return nil
}

func logInvocation(logPath string) {
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	cwd, _ := os.Getwd()
	fmt.Fprintf(f, "invoked: args=%v cwd=%s\n", os.Args, cwd)
}
