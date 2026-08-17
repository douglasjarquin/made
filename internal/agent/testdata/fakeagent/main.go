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
)

func main() {
	if logPath := os.Getenv("FAKE_AGENT_LOG_FILE"); logPath != "" {
		logInvocation(logPath)
	}

	if code := os.Getenv("FAKE_AGENT_EXIT_CODE"); code != "" && code != "0" {
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

	if _, err := os.Stdout.Write(data); err != nil {
		fmt.Fprintf(os.Stderr, "fakeagent: write stdout: %v\n", err)
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
}
