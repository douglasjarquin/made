// Package review is stage 3 of made's pipeline (Intent -> Rebase -> Review ->
// ...): it spawns the configured agent (Claude or Codex, via internal/agent)
// against the diff in a gate worktree, applies auto-fixable findings as new
// commits, and queues ask-user/blocking findings in Result.PendingFindings so
// a later human-approval stage (Task 22) can act on them - findings are never
// silently applied or silently dropped.
package review

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/douglasjarquin/made/internal/agent"
)

type Options struct {
	BinaryPath string
	ExtraEnv   []string
	Timeout    time.Duration
}

type Result struct {
	OK              bool
	Message         string
	AutoFixed       []string
	PendingFindings []agent.Finding
}

// Run's error return is reserved for infrastructure failures (agent spawn
// failure, unparseable agent output, a scripted patch that fails to apply,
// etc); ask-user and blocking findings are normal outcomes reported via
// Result, not errors.
func Run(ctx context.Context, worktreePath string, agentKind agent.Kind, opts Options) (Result, error) {
	findings, err := agent.Spawn(ctx, agentKind, agent.SpawnParams{
		WorktreePath: worktreePath,
		BinaryPath:   opts.BinaryPath,
		ExtraEnv:     opts.ExtraEnv,
		Timeout:      opts.Timeout,
	})
	if err != nil {
		return Result{}, fmt.Errorf("review: spawn %s: %w", agentKind, err)
	}

	var autoFixed []string
	var pending []agent.Finding
	var blockingMessages []string

	for _, finding := range findings.Findings {
		switch finding.Kind {
		case agent.FindingAutoFixable:
			sha, applyErr := applyAutoFix(worktreePath, finding)
			if applyErr != nil {
				return Result{}, fmt.Errorf("review: apply auto-fix %q: %w", finding.Description, applyErr)
			}
			autoFixed = append(autoFixed, sha)
		case agent.FindingBlocking:
			blockingMessages = append(blockingMessages, finding.Description)
			pending = append(pending, finding)
		default:
			pending = append(pending, finding)
		}
	}

	if len(blockingMessages) > 0 {
		return Result{
			OK:              false,
			Message:         fmt.Sprintf("review halted by blocking finding(s): %s", strings.Join(blockingMessages, "; ")),
			AutoFixed:       autoFixed,
			PendingFindings: pending,
		}, nil
	}

	return Result{
		OK:              true,
		Message:         fmt.Sprintf("review passed: %d auto-fix(es) applied, %d finding(s) await human approval", len(autoFixed), len(pending)),
		AutoFixed:       autoFixed,
		PendingFindings: pending,
	}, nil
}

func applyAutoFix(worktreePath string, finding agent.Finding) (string, error) {
	if strings.TrimSpace(finding.Patch) == "" {
		return "", fmt.Errorf("auto-fixable finding has no patch")
	}

	applyCmd := exec.Command("git", "-C", worktreePath, "apply", "--whitespace=fix", "-")
	applyCmd.Stdin = strings.NewReader(finding.Patch)
	if out, err := applyCmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("git apply: %w: %s", err, strings.TrimSpace(string(out)))
	}

	addCmd := exec.Command("git", "-C", worktreePath, "add", "-A")
	if out, err := addCmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("git add -A: %w: %s", err, strings.TrimSpace(string(out)))
	}

	message := finding.Description
	if message == "" {
		message = "made review: auto-fix"
	}
	commitCmd := exec.Command("git", "-C", worktreePath,
		"-c", "user.name=made-review",
		"-c", "user.email=made-review@local",
		"commit", "-m", message)
	if out, err := commitCmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("git commit: %w: %s", err, strings.TrimSpace(string(out)))
	}

	shaOut, err := exec.Command("git", "-C", worktreePath, "rev-parse", "HEAD").Output()
	if err != nil {
		return "", fmt.Errorf("git rev-parse HEAD: %w", err)
	}
	return strings.TrimSpace(string(shaOut)), nil
}
