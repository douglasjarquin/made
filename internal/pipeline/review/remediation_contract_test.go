package review_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/douglasjarquin/made/internal/agent"
	"github.com/douglasjarquin/made/internal/agent/agenttest"
	"github.com/douglasjarquin/made/internal/pipeline/review"
)

func TestRun_AutoFixRequiresCleanStateBeforeApplyingReturnedPatch(t *testing.T) {
	bin := agenttest.Build(t)
	f := setupFixture(t)
	wt := f.addWorktree(t)
	t.Cleanup(func() { _ = wt.Remove() })

	dirtyPath := filepath.Join(wt.Path, "unrelated.txt")
	if err := os.WriteFile(dirtyPath, []byte("unrelated user work\n"), 0o600); err != nil {
		t.Fatalf("write dirty fixture: %v", err)
	}
	patch := autoFixPatch(t, wt.Path)
	scenarioPath := writeScenario(t, agent.Findings{Findings: []agent.Finding{
		{Kind: agent.FindingAutoFixable, Description: "clean-state fix", Patch: patch, Paths: []string{"reviewed.txt"}},
	}})

	if _, err := review.Run(t.Context(), wt.Path, agent.KindCodex, review.Options{
		BinaryPath: bin,
		ExtraEnv:   []string{"FAKE_AGENT_SCENARIO=" + scenarioPath},
	}); err == nil {
		t.Fatal("review auto-fix mutated a dirty worktree instead of refusing before apply")
	}
}
