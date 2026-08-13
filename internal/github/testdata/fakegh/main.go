// Command fakegh is a deterministic test double for the gh CLI: it never
// calls the real GitHub API, it just replays scripted exit codes and output
// driven by env vars, mirroring internal/agent/testdata/fakeagent's approach
// so internal/github can be tested without network access or credentials.
package main

import (
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

	if code := os.Getenv("FAKE_GH_EXIT_CODE"); code != "" && code != "0" {
		fmt.Fprintln(os.Stderr, envOr("FAKE_GH_STDERR", "fakegh: scripted failure"))
		os.Exit(1)
	}

	switch {
	case len(args) >= 2 && args[0] == "pr" && args[1] == "create":
		fmt.Fprintln(os.Stdout, envOr("FAKE_GH_PR_URL", "https://github.com/example/repo/pull/1"))
	case len(args) >= 2 && args[0] == "pr" && args[1] == "view":
		fmt.Fprint(os.Stdout, prViewResponse())
	case len(args) >= 2 && args[0] == "run" && args[1] == "view":
		fmt.Fprint(os.Stdout, envOr("FAKE_GH_RUN_LOG", "log line 1\nlog line 2\n"))
	case len(args) >= 2 && args[0] == "run" && args[1] == "rerun":
	default:
		fmt.Fprintf(os.Stderr, "fakegh: unrecognized args %v\n", args)
		os.Exit(1)
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func prViewResponse() string {
	states := os.Getenv("FAKE_GH_PR_VIEW_STATES")
	if states == "" {
		return envOr("FAKE_GH_PR_VIEW_JSON", `{"mergeStateStatus":"CLEAN"}`)
	}
	list := strings.Split(states, ",")
	idx := nextSequenceIndex("pr_view", len(list))
	return fmt.Sprintf(`{"mergeStateStatus":%q}`, strings.TrimSpace(list[idx]))
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
	_ = os.WriteFile(path, []byte(strconv.Itoa(count+1)), 0o644)
	return idx
}

func logInvocation(logPath string, args []string) {
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "invoked: args=%s\n", strings.Join(args, " "))
}
