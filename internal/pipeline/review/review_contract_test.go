package review_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/douglasjarquin/made/internal/agent"
	"github.com/douglasjarquin/made/internal/agent/agenttest"
	"github.com/douglasjarquin/made/internal/pipeline/review"
)

func TestRun_AutoFixDoesNotStageUnrelatedChanges(t *testing.T) {
	bin := agenttest.Build(t)
	f := setupFixture(t)
	wt := f.addWorktree(t)
	defer func() {
		if err := wt.Remove(); err != nil {
			t.Errorf("Remove: %v", err)
		}
	}()

	writeFile(t, wt.Path, "unrelated.txt", "must not be committed\n")
	run(t, wt.Path, "add", "unrelated.txt")
	patch := autoFixPatch(t, wt.Path)
	scenarioPath := writeScenario(t, agent.Findings{Findings: []agent.Finding{{
		Kind: agent.FindingAutoFixable, Description: "contained fix", Patch: patch, Paths: []string{"reviewed.txt"},
	}}})

	result, err := review.Run(context.Background(), wt.Path, agent.KindCodex, review.Options{
		BinaryPath: bin,
		ExtraEnv: []string{
			"FAKE_AGENT_KIND=codex",
			"FAKE_AGENT_SCENARIO=" + scenarioPath,
		},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(result.AutoFixed) != 1 {
		t.Fatalf("expected one auto-fix commit, got %+v", result)
	}

	files := run(t, wt.Path, "show", "--format=", "--name-only", result.AutoFixed[0])
	if strings.Contains(files, "unrelated.txt") {
		t.Fatalf("unrelated file was included in auto-fix commit: %s", files)
	}
	staged := run(t, wt.Path, "diff", "--cached", "--name-only")
	if !strings.Contains(staged, "unrelated.txt") {
		t.Fatalf("pre-staged unrelated file was lost from the worktree index: %s", staged)
	}
	if _, err := os.Stat(filepath.Join(wt.Path, "unrelated.txt")); err != nil {
		t.Fatalf("unrelated fixture disappeared: %v", err)
	}
}

func TestRun_RejectsUnresolvedCandidateOutputSHA(t *testing.T) {
	bin := agenttest.Build(t)
	f := setupFixture(t)
	wt := f.addWorktree(t)
	t.Cleanup(func() { _ = wt.Remove() })

	_, err := review.Run(context.Background(), wt.Path, agent.KindCodex, review.Options{
		BinaryPath:         bin,
		BaseBranch:         "HEAD",
		CandidateOutputSHA: strings.Repeat("d", 40),
	})
	if err == nil || !strings.Contains(err.Error(), "does not match the current review candidate") {
		t.Fatalf("review.Run error = %v, want unresolved candidate output rejection", err)
	}
}
