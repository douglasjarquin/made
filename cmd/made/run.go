package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/douglasjarquin/made/internal/api"
	"github.com/douglasjarquin/made/internal/daemon"
)

func runRunCommand(args []string, stdout, stderr *os.File) int {
	if len(args) < 1 {
		_, _ = fmt.Fprintln(stderr, "usage: made run <submit|status|list|cancel> [args]")
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

type runSubmitFlags struct {
	RunID        string
	Repo         string
	Branch       string
	Ref          string
	OldSHA       string
	InputSHA     string
	OutputSHA    string
	SubmissionID string
	GatePath     string
}

func runSubmitCommand(args []string, stdout, stderr *os.File) int {
	fs := flag.NewFlagSet("made run submit", flag.ContinueOnError)
	fs.SetOutput(stderr)
	flags := runSubmitFlags{}
	fs.StringVar(&flags.RunID, "run-id", "", "exact run ID")
	fs.StringVar(&flags.Repo, "repo", "", "repository identity")
	fs.StringVar(&flags.Branch, "branch", "", "branch identity")
	fs.StringVar(&flags.Ref, "ref", "", "git ref")
	fs.StringVar(&flags.OldSHA, "old-sha", "", "previous input SHA")
	fs.StringVar(&flags.InputSHA, "input-sha", "", "input SHA")
	fs.StringVar(&flags.OutputSHA, "output-sha", "", "output SHA")
	fs.StringVar(&flags.SubmissionID, "submission-id", "", "submission identity")
	fs.StringVar(&flags.GatePath, "gate", "", "gate path")
	jsonOutput := fs.Bool("json", false, "output JSON")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 0 || flags.Repo == "" || flags.Branch == "" {
		_, _ = fmt.Fprintln(stderr, "usage: made run submit --repo <repo> --branch <branch> [--run-id <id>] [--json]")
		return 2
	}

	result, err := callDaemon("run.submit", daemon.RunSubmission{
		ID:           flags.RunID,
		Repo:         flags.Repo,
		Branch:       flags.Branch,
		Ref:          flags.Ref,
		OldSHA:       flags.OldSHA,
		InputSHA:     flags.InputSHA,
		OutputSHA:    flags.OutputSHA,
		SubmissionID: flags.SubmissionID,
		GatePath:     flags.GatePath,
	})
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "made run submit:", err)
		return 1
	}
	var snapshot daemon.RunSnapshot
	if err := json.Unmarshal(result, &snapshot); err != nil {
		_, _ = fmt.Fprintln(stderr, "made run submit: decode response:", err)
		return 1
	}
	return writeRunOutput(snapshot, *jsonOutput, stdout, stderr)
}

func runExactStatusCommand(args []string, stdout, stderr *os.File) int {
	fs := flag.NewFlagSet("made run status", flag.ContinueOnError)
	fs.SetOutput(stderr)
	jsonOutput := fs.Bool("json", false, "output JSON")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() == 2 && fs.Arg(1) == "--json" {
		*jsonOutput = true
	}
	if (fs.NArg() != 1 && fs.NArg() != 2) || fs.Arg(0) == "" {
		_, _ = fmt.Fprintln(stderr, "usage: made run status <exact-run-id> [--json]")
		return 2
	}
	result, err := callDaemon("run.status", statusParams{RunID: fs.Arg(0)})
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "made run status:", err)
		return 1
	}
	var report StatusReport
	if err := json.Unmarshal(result, &report); err != nil {
		_, _ = fmt.Fprintln(stderr, "made run status: decode response:", err)
		return 1
	}
	if *jsonOutput {
		return writeJSON(report, stdout, stderr)
	}
	_, _ = fmt.Fprintf(stdout, "%s %s\n", report.RunID, report.State)
	return 0
}

func runListCommand(args []string, stdout, stderr *os.File) int {
	fs := flag.NewFlagSet("made run list", flag.ContinueOnError)
	fs.SetOutput(stderr)
	active := fs.Bool("active", false, "list only non-terminal runs")
	jsonOutput := fs.Bool("json", false, "output JSON")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 0 {
		_, _ = fmt.Fprintln(stderr, "usage: made run list [--active] [--json]")
		return 2
	}
	result, err := callDaemon("run.list", struct {
		Active bool `json:"active"`
	}{Active: *active})
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "made run list:", err)
		return 1
	}
	var reports []StatusReport
	if err := json.Unmarshal(result, &reports); err != nil {
		_, _ = fmt.Fprintln(stderr, "made run list: decode response:", err)
		return 1
	}
	if *jsonOutput {
		return writeJSON(reports, stdout, stderr)
	}
	for _, report := range reports {
		_, _ = fmt.Fprintf(stdout, "%s %s\n", report.RunID, report.State)
	}
	return 0
}

func runCancelCommand(args []string, stdout, stderr *os.File) int {
	fs := flag.NewFlagSet("made run cancel", flag.ContinueOnError)
	fs.SetOutput(stderr)
	jsonOutput := fs.Bool("json", false, "output JSON")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() == 2 && fs.Arg(1) == "--json" {
		*jsonOutput = true
	}
	if (fs.NArg() != 1 && fs.NArg() != 2) || fs.Arg(0) == "" {
		_, _ = fmt.Fprintln(stderr, "usage: made run cancel <exact-run-id> [--json]")
		return 2
	}
	result, err := callDaemon("run.cancel", map[string]string{"run_id": fs.Arg(0)})
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "made run cancel:", err)
		return 1
	}
	if *jsonOutput {
		return writeJSON(json.RawMessage(result), stdout, stderr)
	}
	_, _ = fmt.Fprintln(stdout, "canceled:", fs.Arg(0))
	return 0
}

func callDaemon(method string, params any) (json.RawMessage, error) {
	home, err := madeHome()
	if err != nil {
		return nil, err
	}
	client, err := api.Dial(api.SocketPath(home))
	if err != nil {
		return nil, fmt.Errorf("daemon not reachable: %w", err)
	}
	defer func() { _ = client.Close() }()
	return client.Call(method, params)
}

func writeRunOutput(snapshot daemon.RunSnapshot, jsonOutput bool, stdout, stderr *os.File) int {
	if jsonOutput {
		return writeJSON(newStatusReport(snapshot), stdout, stderr)
	}
	_, _ = fmt.Fprintf(stdout, "%s %s\n", snapshot.ID, snapshot.Status)
	return 0
}

func writeJSON(value any, stdout, stderr *os.File) int {
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		_, _ = fmt.Fprintln(stderr, "encode JSON:", err)
		return 1
	}
	return 0
}
