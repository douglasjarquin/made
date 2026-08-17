// Command fakegh is a deterministic test double for the gh CLI: it never
// calls the real GitHub API, it just replays scripted exit codes and output
// driven by env vars, mirroring internal/agent/testdata/fakeagent's approach
// so internal/github can be tested without network access or credentials.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func main() {
	args := os.Args[1:]

	if logPath := os.Getenv("FAKE_GH_LOG_FILE"); logPath != "" {
		logInvocation(logPath, args)
	}

	if len(args) >= 2 && args[0] == "auth" && args[1] == "status" {
		if code := os.Getenv("FAKE_GH_AUTH_EXIT_CODE"); code != "" && code != "0" {
			fmt.Fprintln(os.Stderr, envOr("FAKE_GH_AUTH_STDERR", "You are not logged into any GitHub hosts."))
			os.Exit(1)
		}
		fmt.Fprintln(os.Stdout, "Logged in to github.com account fakeuser")
		return
	}

	switch {
	case len(args) >= 2 && args[0] == "pr" && args[1] == "create":
		if !validPRCreateArgs(args[2:]) {
			reject(args)
		}
		failIfScripted()
		fmt.Fprintln(os.Stdout, envOr("FAKE_GH_PR_URL", "https://github.com/example/repo/pull/1"))
	case len(args) == 5 && args[0] == "pr" && args[1] == "checks" && args[3] == "--json" && args[4] == "name,state,bucket,link":
		payload := checksResponse()
		fmt.Fprint(os.Stdout, payload)
		if code := envExitCode("FAKE_GH_CHECKS_EXIT_CODE"); code != 0 {
			os.Exit(code)
		}
		if checksFail(payload) {
			os.Exit(1)
		}
	case len(args) == 4 && args[0] == "run" && args[1] == "view" && isRunID(args[2]) && args[3] == "--log":
		failIfScripted()
		fmt.Fprint(os.Stdout, envOr("FAKE_GH_RUN_LOG", "log line 1\nlog line 2\n"))
	case len(args) == 4 && args[0] == "run" && args[1] == "rerun" && isRunID(args[2]) && args[3] == "--failed":
		failIfScripted()
	default:
		reject(args)
	}
}

func reject(args []string) {
	fmt.Fprintf(os.Stderr, "fakegh: unrecognized args %v\n", args)
	os.Exit(2)
}

func failIfScripted() {
	if code := envExitCode("FAKE_GH_EXIT_CODE"); code != 0 {
		fmt.Fprintln(os.Stderr, envOr("FAKE_GH_STDERR", "fakegh: scripted failure"))
		os.Exit(code)
	}
}

func envExitCode(key string) int {
	code := os.Getenv(key)
	if code == "" || code == "0" {
		return 0
	}
	n, err := strconv.Atoi(code)
	if err != nil || n < 1 || n > 125 {
		return 1
	}
	return n
}

func isRunID(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func validPRCreateArgs(args []string) bool {
	if len(args) < 4 || len(args)%2 != 0 {
		return false
	}
	seen := map[string]bool{}
	for i := 0; i < len(args); i += 2 {
		if args[i] != "--title" && args[i] != "--body" && args[i] != "--base" && args[i] != "--head" {
			return false
		}
		if seen[args[i]] || args[i+1] == "" {
			return false
		}
		seen[args[i]] = true
	}
	return seen["--title"] && seen["--body"]
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func checksResponse() string {
	raw := envOr("FAKE_GH_CHECKS_JSON", `[{"name":"build","state":"COMPLETED","bucket":"pass","link":"https://github.com/example/repo/actions/runs/12345"}]`)
	var checks []map[string]string
	if err := json.Unmarshal([]byte(raw), &checks); err != nil {
		fmt.Fprintf(os.Stderr, "fakegh: invalid FAKE_GH_CHECKS_JSON: %v\n", err)
		os.Exit(2)
	}
	if sequence := os.Getenv("FAKE_GH_CHECKS_BUCKETS"); sequence != "" {
		buckets := strings.Split(sequence, ",")
		bucket := strings.TrimSpace(buckets[nextSequenceIndex("checks", len(buckets))])
		for _, check := range checks {
			check["bucket"] = bucket
			check["state"] = "COMPLETED"
		}
	}
	data, err := json.Marshal(checks)
	if err != nil {
		fmt.Fprintf(os.Stderr, "fakegh: encode checks: %v\n", err)
		os.Exit(2)
	}
	return string(data)
}

func checksFail(raw string) bool {
	var checks []map[string]string
	if err := json.Unmarshal([]byte(raw), &checks); err != nil {
		return true
	}
	for _, check := range checks {
		if check["bucket"] != "pass" {
			return true
		}
	}
	return false
}

// nextSequenceIndex lets one scripted state sequence (e.g. "fails twice then
// passes") span multiple fakegh invocations, since gh polling means each
// status check is a brand-new process with no memory of the last one. It
// persists a call counter to FAKE_GH_STATE_DIR and clamps to the last index
// once the sequence is exhausted, so an "always fails" script is just a
// single-element list and never needs a state dir at all.
func nextSequenceIndex(name string, length int) int {
	if length <= 1 {
		return 0
	}
	dir := os.Getenv("FAKE_GH_STATE_DIR")
	if dir == "" {
		return 0
	}
	path := filepath.Join(dir, name+".count")
	count := 0
	if data, err := os.ReadFile(path); err == nil {
		if n, err := strconv.Atoi(strings.TrimSpace(string(data))); err == nil {
			count = n
		}
	}
	idx := count
	if idx >= length {
		idx = length - 1
	}
	if err := os.WriteFile(path, []byte(strconv.Itoa(count+1)), 0o644); err != nil {
		return idx
	}
	return idx
}

func logInvocation(logPath string, args []string) {
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer func() { _ = f.Close() }()
	fmt.Fprintf(f, "invoked: args=%s\n", strings.Join(args, " "))
}
