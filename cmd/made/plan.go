package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/douglasjarquin/made/internal/config"
	"github.com/douglasjarquin/made/internal/exec"
	"github.com/douglasjarquin/made/internal/planner"
)

const planCommandTimeout = 30 * time.Second

func runPlanCommand(args []string, stdout, stderr *os.File) int {
	fs := flag.NewFlagSet("made plan", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "output structured JSON")
	base := fs.String("base", "", "base branch/ref to diff against (defaults to origin/HEAD, then main)")
	candidate := fs.String("candidate", "HEAD", "candidate ref to diff (defaults to HEAD)")
	repoPath := fs.String("repo", ".", "path to the repository to plan")
	configPath := fs.String("config", "", "path to a .made.yml to use as the effective config (defaults to <repo>/.made.yml)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 0 {
		_, _ = fmt.Fprintln(stderr, "usage: made plan [--json] [--base <ref>] [--candidate <ref>] [--repo <path>]")
		return 2
	}

	ctx, cancel := context.WithTimeout(context.Background(), planCommandTimeout)
	defer cancel()

	resolvedBase := *base
	if resolvedBase == "" {
		var err error
		resolvedBase, err = resolveDefaultBaseBranch(ctx, *repoPath)
		if err != nil {
			_, _ = fmt.Fprintln(stderr, "made plan: resolve default base branch:", err)
			return 1
		}
	}

	path := *configPath
	if path == "" {
		path = *repoPath + "/.made.yml"
	}
	cfg, err := config.LoadEffectiveConfig(path, path)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "made plan: load effective config:", err)
		return 1
	}

	plan, err := planner.BuildPlan(ctx, *repoPath, resolvedBase, *candidate, cfg)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "made plan:", err)
		return 1
	}

	if *asJSON {
		encoder := json.NewEncoder(stdout)
		if err := encoder.Encode(plan); err != nil {
			_, _ = fmt.Fprintln(stderr, "made plan:", err)
			return 1
		}
		return 0
	}

	printHumanPlan(stdout, plan)
	return 0
}

func printHumanPlan(stdout *os.File, plan planner.Plan) {
	_, _ = fmt.Fprintf(stdout, "Candidate: %s\n", plan.CandidateSHA)
	_, _ = fmt.Fprintf(stdout, "Base:      %s\n", plan.BaseSHA)
	_, _ = fmt.Fprintf(stdout, "Config:    %s\n\n", plan.ConfigHash)

	if len(plan.ChangedPaths) == 0 {
		_, _ = fmt.Fprintln(stdout, "Changed paths: (none)")
	} else {
		_, _ = fmt.Fprintln(stdout, "Changed paths:")
		for _, p := range plan.ChangedPaths {
			_, _ = fmt.Fprintf(stdout, "  %s\n", p)
		}
	}
	_, _ = fmt.Fprintln(stdout)

	_, _ = fmt.Fprintln(stdout, "Plan:")
	for _, s := range plan.Stages {
		_, _ = fmt.Fprintf(stdout, "  %-15s %-6s %s\n", stageDisplayName(s.Name), s.Action, s.Reason)
	}
}

func stageDisplayName(name string) string {
	if name == "" {
		return name
	}
	return strings.ToUpper(name[:1]) + name[1:]
}

// resolveDefaultBaseBranch mirrors what a real run resolves as the trusted
// default branch, without needing the daemon or gate: origin/HEAD when the
// repository has it, falling back to "main" for a repo with no such remote
// tracking ref (a fresh local-only repo, most test fixtures).
func resolveDefaultBaseBranch(ctx context.Context, repoPath string) (string, error) {
	res, err := exec.Run(ctx, exec.Command{
		Name: "git",
		Args: []string{"symbolic-ref", "--short", "refs/remotes/origin/HEAD"},
		Dir:  repoPath,
	})
	if err == nil && res.ExitCode == 0 {
		ref := strings.TrimSpace(string(res.Stdout))
		return strings.TrimPrefix(ref, "origin/"), nil
	}
	return "main", nil
}
