package orchestrator

import (
	"context"
	"fmt"
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
