// Command fakeagent is a deterministic test double for the Codex CLI:
// it never calls a real model, it just replays a scripted findings payload so
// internal/agent and internal/pipeline/review can be tested without network
// access or API keys. Scenario selection and invocation logging are both
// driven by env vars (mirroring how the real adapters pass agent config
// through the environment) rather than flags, since internal/exec.Command
// already has an Env field but no generic flag-injection point.
package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

func main() {
	if kind := os.Getenv("FAKE_AGENT_KIND"); kind != "" && kind != string(agentKindCodex) {
		fmt.Fprintln(os.Stderr, "fakeagent: only the codex structured exec contract is supported")
		os.Exit(2)
	}
	if err := validateInvocation(os.Args[1:]); err != nil {
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

	if _, err := os.Stdout.Write(data); err != nil {
		fmt.Fprintf(os.Stderr, "fakeagent: write structured output: %v\n", err)
		os.Exit(1)
	}
}

const agentKindCodex = "codex"

func validateInvocation(args []string) error {
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
