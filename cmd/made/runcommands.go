package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/douglasjarquin/made/internal/agent"
	"github.com/douglasjarquin/made/internal/api"
)

type capabilitiesReport struct {
	SchemaVersion   int      `json:"schema_version"`
	ProtocolVersion int      `json:"protocol_version"`
	Commands        []string `json:"commands"`
	Agents          []string `json:"agents"`
}

func runCapabilitiesCommand(args []string, stdout, stderr *os.File) int {
	fs := flag.NewFlagSet("made capabilities", flag.ContinueOnError)
	fs.SetOutput(stderr)
	jsonOutput := fs.Bool("json", false, "output JSON")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if !*jsonOutput {
		_, _ = fmt.Fprintln(stderr, "made capabilities: --json is required")
		return 2
	}
	return writeJSON(stdout, capabilitiesReport{
		SchemaVersion: 1, ProtocolVersion: api.Version,
		Commands: []string{"run.submit", "run.status", "run.list", "run.cancel", "review.decide", "doctor"},
		Agents:   supportedAgentNames(),
	}, stderr, "made capabilities")
}

func supportedAgentNames() []string {
	kinds := agent.SupportedKinds()
	names := make([]string, len(kinds))
	for index, kind := range kinds {
		names[index] = string(kind)
	}
	return names
}

type runSubmitParams struct {
	RunID     string `json:"run_id,omitempty"`
	GatePath  string `json:"gate_path"`
	Ref       string `json:"ref"`
	OldSHA    string `json:"old_sha,omitempty"`
	Repo      string `json:"repo,omitempty"`
	Branch    string `json:"branch,omitempty"`
	InputSHA  string `json:"input_sha"`
	OutputSHA string `json:"output_sha,omitempty"`
}

type runCancelParams struct {
	RunID string `json:"run_id"`
}

type runListParams struct {
	Active bool `json:"active"`
}

type runListReport struct {
	SchemaVersion   int            `json:"schema_version"`
	ProtocolVersion int            `json:"protocol_version"`
	Runs            []StatusReport `json:"runs"`
}

func runRunCommand(args []string, stdout, stderr *os.File) int {
	if len(args) == 0 {
		_, _ = fmt.Fprintln(stderr, "usage: made run <submit|status|list|cancel>")
		return 2
	}
	switch args[0] {
	case "submit":
		return runSubmitCommand(args[1:], stdout, stderr)
	case "status":
		return runExactStatusCommand(args[1:], stdout, stderr)
	case "list":
		return runListCommand(args[1:], stdout, stderr)
	case "cancel":
		return runCancelCommand(args[1:], stdout, stderr)
	default:
		_, _ = fmt.Fprintf(stderr, "made run: unknown subcommand %q\n", args[0])
		return 2
	}
}

func dialMade(home string, stderr *os.File, label string) (*api.Client, bool) {
	client, err := api.Dial(api.SocketPath(home))
	if err != nil {
		_, _ = fmt.Fprintln(stderr, label+": daemon not reachable:", err)
		return nil, false
	}
	return client, true
}

func runSubmitCommand(args []string, stdout, stderr *os.File) int {
	fs := flag.NewFlagSet("made run submit", flag.ContinueOnError)
	fs.SetOutput(stderr)
	jsonOutput := fs.Bool("json", false, "output JSON")
	gatePath := fs.String("gate", "", "bare Made gate repository path")
	ref := fs.String("ref", "", "immutable branch ref")
	oldSHA := fs.String("old-sha", "", "previous branch head SHA")
	repo := fs.String("repo", "", "repository identity")
	branch := fs.String("branch", "", "input branch")
	inputSHA := fs.String("input-sha", "", "immutable input commit SHA")
	outputSHA := fs.String("output-sha", "", "expected output commit SHA")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if !*jsonOutput {
		_, _ = fmt.Fprintln(stderr, "made run submit: --json is required")
		return 2
	}
	home, err := madeHome()
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "made run submit:", err)
		return 1
	}
	client, ok := dialMade(home, stderr, "made run submit")
	if !ok {
		return 1
	}
	defer func() { _ = client.Close() }()
	var result runActionReport
	if err := client.CallInto("run.submit", runSubmitParams{
		GatePath: *gatePath, Ref: *ref, OldSHA: *oldSHA,
		Repo: *repo, Branch: *branch, InputSHA: *inputSHA, OutputSHA: *outputSHA,
	}, &result); err != nil {
		_, _ = fmt.Fprintln(stderr, "made run submit:", err)
		return 1
	}
	return writeJSON(stdout, result, stderr, "made run submit")
}

func runExactStatusCommand(args []string, stdout, stderr *os.File) int {
	fs := flag.NewFlagSet("made run status", flag.ContinueOnError)
	fs.SetOutput(stderr)
	jsonOutput := fs.Bool("json", false, "output JSON")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if !*jsonOutput || fs.NArg() != 1 {
		_, _ = fmt.Fprintln(stderr, "usage: made run status --json <run-id>")
		return 2
	}
	home, err := madeHome()
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "made run status:", err)
		return 1
	}
	client, ok := dialMade(home, stderr, "made run status")
	if !ok {
		return 1
	}
	defer func() { _ = client.Close() }()
	var report StatusReport
	if err := client.CallInto("run.status", statusParams{RunID: fs.Arg(0)}, &report); err != nil {
		_, _ = fmt.Fprintln(stderr, "made run status:", err)
		return 1
	}
	return writeJSON(stdout, report, stderr, "made run status")
}

func runListCommand(args []string, stdout, stderr *os.File) int {
	fs := flag.NewFlagSet("made run list", flag.ContinueOnError)
	fs.SetOutput(stderr)
	jsonOutput := fs.Bool("json", false, "output JSON")
	active := fs.Bool("active", false, "only active runs")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if !*jsonOutput || fs.NArg() != 0 {
		_, _ = fmt.Fprintln(stderr, "usage: made run list --json [--active]")
		return 2
	}
	home, err := madeHome()
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "made run list:", err)
		return 1
	}
	client, ok := dialMade(home, stderr, "made run list")
	if !ok {
		return 1
	}
	defer func() { _ = client.Close() }()
	var report runListReport
	if err := client.CallInto("run.list", runListParams{Active: *active}, &report); err != nil {
		_, _ = fmt.Fprintln(stderr, "made run list:", err)
		return 1
	}
	return writeJSON(stdout, report, stderr, "made run list")
}

func runCancelCommand(args []string, stdout, stderr *os.File) int {
	fs := flag.NewFlagSet("made run cancel", flag.ContinueOnError)
	fs.SetOutput(stderr)
	jsonOutput := fs.Bool("json", false, "output JSON")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if !*jsonOutput || fs.NArg() != 1 {
		_, _ = fmt.Fprintln(stderr, "usage: made run cancel --json <run-id>")
		return 2
	}
	home, err := madeHome()
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "made run cancel:", err)
		return 1
	}
	client, ok := dialMade(home, stderr, "made run cancel")
	if !ok {
		return 1
	}
	defer func() { _ = client.Close() }()
	var report runActionReport
	if err := client.CallInto("run.cancel", runCancelParams{RunID: fs.Arg(0)}, &report); err != nil {
		_, _ = fmt.Fprintln(stderr, "made run cancel:", err)
		return 1
	}
	return writeJSON(stdout, report, stderr, "made run cancel")
}
