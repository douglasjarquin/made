// Package document is stage 5 of made's pipeline (Intent -> Rebase -> Review
// -> Test -> Document -> ...): it diffs the worktree's current HEAD against
// the base branch and checks the changed files against a fixed set of
// source/doc pattern-pair rules, producing an ask-user finding for each rule
// whose source pattern matches a changed file with no corresponding
// doc-pattern match in the same diff. Findings are a judgment call for a
// human, never auto-applied or silently dropped.
package document

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/douglasjarquin/made/internal/agent"
)

type Rule struct {
	SourcePattern string
	DocPattern    string
}

type Result struct {
	OK       bool
	Message  string
	Findings []agent.Finding
}

// Run's error return is reserved for infrastructure failures (git diff
// failing, worktreePath unreadable, etc); a policy violation is a normal
// outcome reported via Result.Findings, not an error.
func Run(worktreePath, baseBranch string, rules []Rule) (Result, error) {
	return RunContext(context.Background(), worktreePath, baseBranch, rules)
}

func RunContext(ctx context.Context, worktreePath, baseBranch string, rules []Rule) (Result, error) {
	changed, err := changedFiles(ctx, worktreePath, baseBranch)
	if err != nil {
		return Result{}, fmt.Errorf("document: %w", err)
	}

	var findings []agent.Finding
	for _, rule := range rules {
		sourceMatches, err := matchAny(rule.SourcePattern, changed)
		if err != nil {
			return Result{}, fmt.Errorf("document: %w", err)
		}
		if len(sourceMatches) == 0 {
			continue
		}

		docMatches, err := matchAny(rule.DocPattern, changed)
		if err != nil {
			return Result{}, fmt.Errorf("document: %w", err)
		}
		if len(docMatches) > 0 {
			continue
		}

		findings = append(findings, agent.Finding{
			Kind: agent.FindingAskUser,
			Description: fmt.Sprintf(
				"documentation policy violation: %s (matches %q) requires a change matching %q, but none was found in this diff",
				strings.Join(sourceMatches, ", "), rule.SourcePattern, rule.DocPattern,
			),
		})
	}

	if len(findings) > 0 {
		return Result{
			OK:       true,
			Message:  fmt.Sprintf("document: %d documentation policy finding(s) await human approval", len(findings)),
			Findings: findings,
		}, nil
	}

	return Result{
		OK:      true,
		Message: "document: no documentation policy violations found",
	}, nil
}

// RunContextWithBaseSHA is like RunContext but uses an exact commit SHA for the
// diff range instead of a mutable branch name. This is required by managed-validation
// mode, which must use the exact base_sha from the preflight-verified CLI arguments.
func RunContextWithBaseSHA(ctx context.Context, worktreePath, baseSHA string, rules []Rule) (Result, error) {
	changed, err := changedFilesWithRange(ctx, worktreePath, baseSHA+"..HEAD")
	if err != nil {
		return Result{}, fmt.Errorf("document: %w", err)
	}

	var findings []agent.Finding
	for _, rule := range rules {
		sourceMatches, err := matchAny(rule.SourcePattern, changed)
		if err != nil {
			return Result{}, fmt.Errorf("document: %w", err)
		}
		if len(sourceMatches) == 0 {
			continue
		}

		docMatches, err := matchAny(rule.DocPattern, changed)
		if err != nil {
			return Result{}, fmt.Errorf("document: %w", err)
		}
		if len(docMatches) > 0 {
			continue
		}

		findings = append(findings, agent.Finding{
			Kind: agent.FindingAskUser,
			Description: fmt.Sprintf(
				"documentation policy violation: %s (matches %q) requires a change matching %q, but none was found in this diff",
				strings.Join(sourceMatches, ", "), rule.SourcePattern, rule.DocPattern,
			),
		})
	}

	if len(findings) > 0 {
		return Result{
			OK:       true,
			Message:  fmt.Sprintf("document: %d documentation policy finding(s) await human approval", len(findings)),
			Findings: findings,
		}, nil
	}

	return Result{
		OK:      true,
		Message: "document: no documentation policy violations found",
	}, nil
}

func changedFilesWithRange(ctx context.Context, worktreePath, gitRange string) ([]string, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", worktreePath, "diff", "--name-only", gitRange)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("git diff --name-only %s: %w: %s", gitRange, err, strings.TrimSpace(string(out)))
	}

	var files []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line != "" {
			files = append(files, line)
		}
	}
	return files, nil
}

func changedFiles(ctx context.Context, worktreePath, baseBranch string) ([]string, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", worktreePath, "diff", "--name-only", baseBranch+"...HEAD")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("git diff --name-only %s...HEAD: %w: %s", baseBranch, err, strings.TrimSpace(string(out)))
	}

	var files []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line != "" {
			files = append(files, line)
		}
	}
	return files, nil
}

func matchAny(pattern string, files []string) ([]string, error) {
	var matches []string
	for _, f := range files {
		ok, err := filepath.Match(pattern, f)
		if err != nil {
			return nil, fmt.Errorf("match pattern %q against %q: %w", pattern, f, err)
		}
		if ok {
			matches = append(matches, f)
		}
	}
	return matches, nil
}
