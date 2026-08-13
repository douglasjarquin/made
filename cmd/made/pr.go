package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/douglasjarquin/made/internal/github"
	"github.com/douglasjarquin/made/internal/pipeline/pr"
)

// runPRCommand is a stopgap: it wires internal/github.Client and
// internal/pipeline/pr.Run directly, in-process, because no orchestrator
// exists yet (Task 9's run manager is not tied to a real pipeline run) to
// submit a PR-stage run through the daemon. Once that orchestration lands,
// `made pr` should submit a run via the socket API like a real pipeline
// stage instead of calling the stage function from the CLI process.
func runPRCommand(args []string, stdout, stderr *os.File) int {
	fs := flag.NewFlagSet("made pr", flag.ContinueOnError)
	fs.SetOutput(stderr)
	title := fs.String("title", "", "pull request title (required)")
	base := fs.String("base", "", "base branch (required)")
	head := fs.String("head", "", "head branch (required)")
	evidenceRef := fs.String("evidence", "", "evidence reference embedded in the PR body (required)")
	dir := fs.String("dir", "", "git repository directory (default: current directory)")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	ghClient := &github.Client{Dir: *dir}
	result, err := pr.Run(context.Background(), ghClient, pr.Options{
		Title:       *title,
		Base:        *base,
		Head:        *head,
		EvidenceRef: *evidenceRef,
	})
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "made pr:", err)
		return 1
	}
	if !result.OK {
		_, _ = fmt.Fprintln(stderr, "made pr:", result.Message)
		return 1
	}
	_, _ = fmt.Fprintln(stdout, result.Message)
	return 0
}
