package orchestrator

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/douglasjarquin/made/internal/config"
	"github.com/douglasjarquin/made/internal/evidence"
	execpkg "github.com/douglasjarquin/made/internal/exec"
	"github.com/douglasjarquin/made/internal/pipeline/document"
)

func derivePRTitle(worktreePath string) (string, error) {
	res, err := execpkg.Run(context.Background(), execpkg.Command{
		Name: "git",
		Args: []string{"log", "-1", "--format=%s"},
		Dir:  worktreePath,
	})
	if err != nil {
		return "", fmt.Errorf("orchestrator: run git log for PR title: %w", err)
	}
	if res.ExitCode != 0 {
		return "", fmt.Errorf("orchestrator: git log for PR title failed: %s", strings.TrimSpace(string(res.Stderr)))
	}
	return strings.TrimSpace(string(res.Stdout)), nil
}

func deriveOutputSHA(worktreePath string) (string, error) {
	res, err := execpkg.Run(context.Background(), execpkg.Command{
		Name: "git",
		Args: []string{"rev-parse", "HEAD"},
		Dir:  worktreePath,
	})
	if err != nil {
		return "", fmt.Errorf("orchestrator: run git rev-parse HEAD: %w", err)
	}
	if res.ExitCode != 0 {
		return "", fmt.Errorf("orchestrator: git rev-parse HEAD failed: %s", strings.TrimSpace(string(res.Stderr)))
	}
	return strings.TrimSpace(string(res.Stdout)), nil
}

func deriveEvidenceRef(store evidence.Store, runID string) string {
	switch s := store.(type) {
	case *evidence.OrphanBranchStore:
		return s.Location(runID)
	case *evidence.InRepoStore:
		return s.Location(runID)
	default:
		return runID
	}
}

// deriveEvidenceURL resolves a clickable GitHub permalink to a run's
// published evidence, or "" when the store cannot publish evidence, its
// commit tip cannot be resolved, or the origin remote is not GitHub. The URL
// pins the resolved commit rather than the branch name so it keeps showing
// the evidence a PR was actually reviewed against, even after a later run
// advances the evidence branch.
func deriveEvidenceURL(ctx context.Context, store evidence.Store, runID string) string {
	s, ok := store.(*evidence.OrphanBranchStore)
	if !ok {
		return ""
	}
	sha, err := s.CommitSHA(ctx)
	if err != nil || sha == "" {
		return ""
	}
	repoPath := githubRepoPath(originRemoteURL(ctx, s.RepoPath))
	if repoPath == "" {
		return ""
	}
	return "https://github.com/" + repoPath + "/tree/" + sha + "/" + runID
}

func originRemoteURL(ctx context.Context, repoPath string) string {
	res, err := execpkg.Run(ctx, execpkg.Command{
		Name: "git",
		Args: []string{"remote", "get-url", "origin"},
		Dir:  repoPath,
	})
	if err != nil || res.ExitCode != 0 {
		return ""
	}
	return strings.TrimSpace(string(res.Stdout))
}

func githubRepoPath(remote string) string {
	remote = strings.TrimSpace(remote)
	if remote == "" {
		return ""
	}
	if strings.HasPrefix(remote, "git@github.com:") {
		return cleanGitHubRepoPath(strings.TrimPrefix(remote, "git@github.com:"))
	}
	parsed, err := url.Parse(remote)
	if err != nil || !strings.EqualFold(parsed.Host, "github.com") {
		return ""
	}
	return cleanGitHubRepoPath(strings.TrimPrefix(parsed.Path, "/"))
}

func cleanGitHubRepoPath(repo string) string {
	repo = strings.TrimSuffix(strings.TrimSpace(repo), ".git")
	parts := strings.Split(repo, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return ""
	}
	if strings.ContainsAny(repo, "\n\r <>[]()\\") || strings.Contains(repo, "..") {
		return ""
	}
	return url.PathEscape(parts[0]) + "/" + url.PathEscape(parts[1])
}

func deriveDocumentRules(cfg config.Config) []document.Rule {
	rules := make([]document.Rule, len(cfg.Document.Rules))
	for i, r := range cfg.Document.Rules {
		rules[i] = document.Rule{
			SourcePattern: r.PathPattern,
			DocPattern:    r.RequiredDocPattern,
		}
	}
	return rules
}
