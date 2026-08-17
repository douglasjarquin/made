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
	"path/filepath"
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
	Findings        []agent.Finding
	AutoFixed       []string
	PreFixSHAs      []string
	PostFixSHAs     []string
	PendingFindings []agent.Finding
}

// Run's error return is reserved for infrastructure failures (agent spawn
// failure, unparseable agent output, a scripted patch that fails to apply,
// etc); ask-user and blocking findings are normal outcomes reported via
// Result, not errors.
func Run(ctx context.Context, worktreePath string, agentKind agent.Kind, opts Options) (Result, error) {
	if err := requireCleanWorktree(ctx, worktreePath); err != nil {
		return Result{}, fmt.Errorf("review: inspect worktree before agent: %w", err)
	}
	findings, err := agent.Spawn(ctx, agentKind, agent.SpawnParams{
		WorktreePath: worktreePath,
		BinaryPath:   opts.BinaryPath,
		ExtraEnv:     opts.ExtraEnv,
		Timeout:      opts.Timeout,
	})
	if err != nil {
		return Result{}, fmt.Errorf("review: spawn %s: %w", agentKind, err)
	}
	if err := requireCleanWorktree(ctx, worktreePath); err != nil {
		return Result{}, fmt.Errorf("review: agent modified worktree: %w", err)
	}

	var autoFixed []string
	var preFixSHAs []string
	var postFixSHAs []string
	var pending []agent.Finding
	var blockingMessages []string

	for _, finding := range findings.Findings {
		switch finding.Kind {
		case agent.FindingAutoFixable:
			preSHA, postSHA, applyErr := applyAutoFix(ctx, worktreePath, finding)
			if applyErr != nil {
				return Result{}, fmt.Errorf("review: apply auto-fix %q: %w", finding.Description, applyErr)
			}
			autoFixed = append(autoFixed, postSHA)
			preFixSHAs = append(preFixSHAs, preSHA)
			postFixSHAs = append(postFixSHAs, postSHA)
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
			Findings:        findings.Findings,
			AutoFixed:       autoFixed,
			PreFixSHAs:      preFixSHAs,
			PostFixSHAs:     postFixSHAs,
			PendingFindings: pending,
		}, nil
	}

	return Result{
		OK:              true,
		Message:         fmt.Sprintf("review passed: %d auto-fix(es) applied, %d finding(s) await human approval", len(autoFixed), len(pending)),
		Findings:        findings.Findings,
		AutoFixed:       autoFixed,
		PreFixSHAs:      preFixSHAs,
		PostFixSHAs:     postFixSHAs,
		PendingFindings: pending,
	}, nil
}

func applyAutoFix(ctx context.Context, worktreePath string, finding agent.Finding) (string, string, error) {
	if strings.TrimSpace(finding.Patch) == "" {
		return "", "", fmt.Errorf("auto-fixable finding has no patch")
	}
	if err := requireCleanWorktree(ctx, worktreePath); err != nil {
		return "", "", err
	}
	preSHA, err := gitOutput(ctx, worktreePath, "rev-parse", "HEAD")
	if err != nil {
		return "", "", fmt.Errorf("record pre-fix SHA: %w", err)
	}
	paths, err := patchPaths(finding.Patch)
	if err != nil {
		return "", "", err
	}
	allowed := make(map[string]struct{}, len(finding.Paths))
	for _, path := range finding.Paths {
		clean, err := cleanReturnedPath(path)
		if err != nil {
			return "", "", err
		}
		if _, err := gitOutput(ctx, worktreePath, "ls-files", "--error-unmatch", "--", clean); err != nil {
			return "", "", fmt.Errorf("auto-fix returned untracked or unauthorized path %q", clean)
		}
		allowed[clean] = struct{}{}
	}
	if len(allowed) == 0 {
		return "", "", fmt.Errorf("auto-fixable finding must return paths")
	}
	for _, path := range paths {
		if _, ok := allowed[path]; !ok {
			return "", "", fmt.Errorf("auto-fix patch changes path %q outside returned paths", path)
		}
	}

	if _, err := runGit(ctx, worktreePath, []string{"apply", "--whitespace=fix", "-"}, []byte(finding.Patch)); err != nil {
		return "", "", fmt.Errorf("git apply: %w", err)
	}

	status, err := gitOutput(ctx, worktreePath, "status", "--porcelain", "--untracked-files=all")
	if err != nil {
		return "", "", fmt.Errorf("inspect post-fix paths: %w", err)
	}
	changed := statusPaths(status)
	for _, path := range changed {
		if _, ok := allowed[path]; !ok {
			return "", "", fmt.Errorf("auto-fix changed forbidden or unreturned path %q", path)
		}
	}
	addArgs := []string{"-C", worktreePath, "add", "--"}
	for path := range allowed {
		addArgs = append(addArgs, path)
	}
	if _, err := runGit(ctx, worktreePath, addArgs[2:], nil); err != nil {
		return "", "", fmt.Errorf("git add returned paths: %w", err)
	}

	message := finding.Description
	if message == "" {
		message = "made review: auto-fix"
	}
	if _, err := runGit(ctx, worktreePath, []string{
		"-c", "user.name=made-review",
		"-c", "user.email=made-review@local",
		"-c", "commit.gpgsign=false",
		"commit", "-m", message,
	}, nil); err != nil {
		return "", "", fmt.Errorf("git commit: %w", err)
	}

	shaOut, err := gitOutput(ctx, worktreePath, "rev-parse", "HEAD")
	if err != nil {
		return "", "", fmt.Errorf("git rev-parse HEAD: %w", err)
	}
	if _, err := gitOutput(ctx, worktreePath, "diff", "--check", preSHA, shaOut); err != nil {
		return "", "", fmt.Errorf("rerun review validation: %w", err)
	}
	return preSHA, shaOut, nil
}

func requireCleanWorktree(ctx context.Context, worktreePath string) error {
	status, err := gitOutput(ctx, worktreePath, "status", "--porcelain", "--untracked-files=all")
	if err != nil {
		return fmt.Errorf("inspect clean worktree: %w", err)
	}
	if strings.TrimSpace(status) != "" {
		return fmt.Errorf("auto-fix requires a clean worktree")
	}
	return nil
}

func patchPaths(patch string) ([]string, error) {
	seen := make(map[string]struct{})
	var oldPath string
	for _, line := range strings.Split(patch, "\n") {
		if strings.HasPrefix(line, "--- ") {
			var err error
			oldPath, err = patchHeaderPath(strings.TrimPrefix(line, "--- "))
			if err != nil {
				return nil, err
			}
			continue
		}
		if !strings.HasPrefix(line, "+++ ") {
			continue
		}
		newPath, err := patchHeaderPath(strings.TrimPrefix(line, "+++ "))
		if err != nil {
			return nil, err
		}
		if oldPath != "" {
			seen[oldPath] = struct{}{}
		}
		if newPath != "" {
			seen[newPath] = struct{}{}
		}
		oldPath = ""
	}
	if len(seen) == 0 {
		return nil, fmt.Errorf("auto-fix patch contains no returned paths")
	}
	paths := make([]string, 0, len(seen))
	for path := range seen {
		paths = append(paths, path)
	}
	return paths, nil
}

func patchHeaderPath(path string) (string, error) {
	fields := strings.Fields(path)
	if len(fields) == 0 {
		return "", fmt.Errorf("auto-fix patch contains an empty file header")
	}
	path = fields[0]
	if path == "/dev/null" {
		return "", nil
	}
	if strings.HasPrefix(path, "a/") || strings.HasPrefix(path, "b/") {
		path = path[2:]
	}
	return cleanReturnedPath(path)
}

func cleanReturnedPath(path string) (string, error) {
	clean := filepath.Clean(path)
	if clean == "." || filepath.IsAbs(path) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) || clean == ".git" || strings.HasPrefix(clean, ".git"+string(filepath.Separator)) {
		return "", fmt.Errorf("auto-fix returned forbidden path %q", path)
	}
	return filepath.ToSlash(clean), nil
}

func statusPaths(status string) []string {
	var paths []string
	for _, line := range strings.Split(status, "\n") {
		line = strings.TrimSpace(line)
		if len(line) < 4 {
			continue
		}
		path := strings.TrimSpace(line[2:])
		if strings.Contains(path, " -> ") {
			path = strings.TrimSpace(strings.SplitN(path, " -> ", 2)[1])
		}
		paths = append(paths, filepath.ToSlash(path))
	}
	return paths
}
