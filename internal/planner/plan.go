// Package planner builds a side-effect-free, explainable execution plan for
// a candidate: which stages will run, which will skip, and why. It never
// runs a stage, mutates git state, or talks to GitHub - it only reads.
//
// This is a Phase 1 (see project issue #33) implementation: a single
// catch-all "default" validation lane stands in for the language-aware lane
// configuration a later phase adds, so the plan preserves today's
// full-pipeline behavior exactly (every configured stage still actually
// runs regardless of what the plan says) while giving an operator a
// concrete, auditable answer for "why would this stage run or skip".
package planner

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/douglasjarquin/made/internal/config"
	"github.com/douglasjarquin/made/internal/exec"
)

// PlanVersion is bumped whenever the Plan or StageDecision shape changes in
// a way a consumer of `made plan --json` should notice.
const PlanVersion = 1

const defaultLaneName = "default"

type Plan struct {
	PlanVersion  int             `json:"plan_version"`
	RepoPath     string          `json:"repo_path"`
	BaseBranch   string          `json:"base_branch"`
	BaseSHA      string          `json:"base_sha"`
	CandidateSHA string          `json:"candidate_sha"`
	ConfigHash   string          `json:"config_hash"`
	ChangedPaths []string        `json:"changed_paths"`
	Stages       []StageDecision `json:"stages"`
}

type StageDecision struct {
	Name   string `json:"name"`
	Lane   string `json:"lane,omitempty"`
	Action string `json:"action"`
	Reason string `json:"reason"`
}

// BuildPlan computes a Plan for the diff between baseRef and candidateRef in
// the repository at repoPath, using cfg as the already-resolved effective
// configuration. It runs only read-only git commands.
func BuildPlan(ctx context.Context, repoPath, baseRef, candidateRef string, cfg config.Config) (Plan, error) {
	baseSHA, err := resolveGitRef(ctx, repoPath, baseRef)
	if err != nil {
		return Plan{}, fmt.Errorf("planner: resolve base ref %q: %w", baseRef, err)
	}
	candidateSHA, err := resolveGitRef(ctx, repoPath, candidateRef)
	if err != nil {
		return Plan{}, fmt.Errorf("planner: resolve candidate ref %q: %w", candidateRef, err)
	}
	changedPaths, err := changedPathsBetween(ctx, repoPath, baseSHA, candidateSHA)
	if err != nil {
		return Plan{}, fmt.Errorf("planner: compute changed paths: %w", err)
	}
	configHash, err := hashConfig(cfg)
	if err != nil {
		return Plan{}, fmt.Errorf("planner: hash effective config: %w", err)
	}

	return Plan{
		PlanVersion:  PlanVersion,
		RepoPath:     repoPath,
		BaseBranch:   baseRef,
		BaseSHA:      baseSHA,
		CandidateSHA: candidateSHA,
		ConfigHash:   configHash,
		ChangedPaths: changedPaths,
		Stages:       buildStageDecisions(changedPaths),
	}, nil
}

func buildStageDecisions(changedPaths []string) []StageDecision {
	defaultLaneAction, defaultLaneReason := "run", fmt.Sprintf("%d path(s) matched lane %q", len(changedPaths), defaultLaneName)
	if len(changedPaths) == 0 {
		defaultLaneAction, defaultLaneReason = "skip", "no changed paths"
	}

	return []StageDecision{
		{Name: "intent", Action: "run", Reason: "built-in precondition"},
		{Name: "rebase", Action: "run", Reason: "branch operation is never reused"},
		{Name: "review", Action: "run", Reason: "AI review is not reusable"},
		{Name: "test", Lane: defaultLaneName, Action: defaultLaneAction, Reason: defaultLaneReason},
		{Name: "document", Action: "run", Reason: "built-in precondition"},
		{Name: "lint", Lane: defaultLaneName, Action: defaultLaneAction, Reason: defaultLaneReason},
		{Name: "push", Action: "run", Reason: "external side effect"},
		{Name: "pr", Action: "run", Reason: "external side effect"},
		{Name: "ci", Action: "run", Reason: "remote evidence"},
	}
}

func hashConfig(cfg config.Config) (string, error) {
	data, err := json.Marshal(cfg)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func resolveGitRef(ctx context.Context, repoPath, ref string) (string, error) {
	res, err := exec.Run(ctx, exec.Command{
		Name: "git",
		Args: []string{"rev-parse", "--verify", ref + "^{commit}"},
		Dir:  repoPath,
	})
	if err != nil {
		return "", err
	}
	if res.ExitCode != 0 {
		return "", fmt.Errorf("git rev-parse %s failed: %s", ref, strings.TrimSpace(string(res.Stderr)))
	}
	return strings.TrimSpace(string(res.Stdout)), nil
}

// changedPathsBetween returns every path the base..candidate diff touched,
// deduplicated and sorted. A rename or copy contributes both its old and
// new path, so a lane matching either name selects correctly.
func changedPathsBetween(ctx context.Context, repoPath, baseSHA, candidateSHA string) ([]string, error) {
	res, err := exec.Run(ctx, exec.Command{
		Name: "git",
		Args: []string{"diff", "--name-status", "-z", baseSHA + "..." + candidateSHA},
		Dir:  repoPath,
	})
	if err != nil {
		return nil, err
	}
	if res.ExitCode != 0 {
		return nil, fmt.Errorf("git diff --name-status failed: %s", strings.TrimSpace(string(res.Stderr)))
	}

	seen := map[string]struct{}{}
	scanner := bufio.NewScanner(strings.NewReader(string(res.Stdout)))
	scanner.Split(splitOnNUL)
	var fields []string
	for scanner.Scan() {
		field := scanner.Text()
		if field == "" {
			continue
		}
		fields = append(fields, field)
	}
	for i := 0; i < len(fields); i++ {
		status := fields[i]
		switch {
		case strings.HasPrefix(status, "R"), strings.HasPrefix(status, "C"):
			if i+2 >= len(fields) {
				continue
			}
			seen[fields[i+1]] = struct{}{}
			seen[fields[i+2]] = struct{}{}
			i += 2
		default:
			if i+1 >= len(fields) {
				continue
			}
			seen[fields[i+1]] = struct{}{}
			i++
		}
	}

	paths := make([]string, 0, len(seen))
	for p := range seen {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	return paths, nil
}

func splitOnNUL(data []byte, atEOF bool) (advance int, token []byte, err error) {
	for i, b := range data {
		if b == 0 {
			return i + 1, data[:i], nil
		}
	}
	if atEOF && len(data) > 0 {
		return len(data), data, nil
	}
	if atEOF {
		return 0, nil, nil
	}
	return 0, nil, nil
}
