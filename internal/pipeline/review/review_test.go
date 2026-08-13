package review_test

import (
	"context"
	"strings"
	"testing"

	"github.com/douglasjarquin/made/internal/agent"
	"github.com/douglasjarquin/made/internal/agent/agenttest"
	"github.com/douglasjarquin/made/internal/pipeline/review"
)

func TestRun_AutoFixApplied(t *testing.T) {
	bin := agenttest.Build(t)
	f := setupFixture(t)
	wt := f.addWorktree(t)
	defer func() {
		if err := wt.Remove(); err != nil {
			t.Errorf("Remove: %v", err)
		}
	}()

	beforeSHA := headSHA(t, wt.Path)
	patch := autoFixPatch(t, wt.Path)

	scenarioPath := writeScenario(t, agent.Findings{
		Findings: []agent.Finding{
			{Kind: agent.FindingAutoFixable, Description: "append auto-fixed line", Patch: patch},
		},
	})

	result, err := review.Run(context.Background(), wt.Path, agent.KindClaude, review.Options{
		BinaryPath: bin,
		ExtraEnv:   []string{"FAKE_AGENT_SCENARIO=" + scenarioPath},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !result.OK {
		t.Fatalf("expected OK=true, got %+v", result)
	}
	if len(result.AutoFixed) != 1 {
		t.Fatalf("expected 1 auto-fixed entry, got %+v", result.AutoFixed)
	}
	if len(result.PendingFindings) != 0 {
		t.Fatalf("expected no pending findings, got %+v", result.PendingFindings)
	}

	afterSHA := headSHA(t, wt.Path)
	if afterSHA == beforeSHA {
		t.Fatalf("expected a new commit in the worktree, HEAD unchanged at %s", beforeSHA)
	}
	if result.AutoFixed[0] != afterSHA {
		t.Fatalf("expected AutoFixed[0]=%s (new HEAD), got %s", afterSHA, result.AutoFixed[0])
	}

	log := run(t, wt.Path, "log", "-1", "--format=%s")
	if !strings.Contains(log, "append auto-fixed line") {
		t.Fatalf("expected commit message to reference the finding, got %q", log)
	}
}

func TestRun_AskUserFindingQueued(t *testing.T) {
	bin := agenttest.Build(t)
	f := setupFixture(t)
	wt := f.addWorktree(t)
	defer func() {
		if err := wt.Remove(); err != nil {
			t.Errorf("Remove: %v", err)
		}
	}()

	beforeSHA := headSHA(t, wt.Path)

	scenarioPath := writeScenario(t, agent.Findings{
		Findings: []agent.Finding{
			{Kind: agent.FindingAskUser, Description: "consider renaming this function"},
		},
	})

	result, err := review.Run(context.Background(), wt.Path, agent.KindClaude, review.Options{
		BinaryPath: bin,
		ExtraEnv:   []string{"FAKE_AGENT_SCENARIO=" + scenarioPath},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(result.AutoFixed) != 0 {
		t.Fatalf("expected no auto-fixes, got %+v", result.AutoFixed)
	}
	if len(result.PendingFindings) != 1 {
		t.Fatalf("expected 1 pending finding, got %+v", result.PendingFindings)
	}
	if result.PendingFindings[0].Description != "consider renaming this function" {
		t.Fatalf("expected pending finding description to be preserved, got %+v", result.PendingFindings[0])
	}
	if result.PendingFindings[0].Kind != agent.FindingAskUser {
		t.Fatalf("expected pending finding kind ask-user, got %q", result.PendingFindings[0].Kind)
	}

	afterSHA := headSHA(t, wt.Path)
	if afterSHA != beforeSHA {
		t.Fatalf("expected no new commit for an ask-user finding, HEAD moved from %s to %s", beforeSHA, afterSHA)
	}
	if !result.OK {
		t.Fatalf("expected an ask-user finding to not halt the stage (queued, not blocking), got %+v", result)
	}
}

func TestRun_BlockingFindingHaltsStage(t *testing.T) {
	bin := agenttest.Build(t)
	f := setupFixture(t)
	wt := f.addWorktree(t)
	defer func() {
		if err := wt.Remove(); err != nil {
			t.Errorf("Remove: %v", err)
		}
	}()

	scenarioPath := writeScenario(t, agent.Findings{
		Findings: []agent.Finding{
			{Kind: agent.FindingBlocking, Description: "hardcoded credential detected"},
		},
	})

	result, err := review.Run(context.Background(), wt.Path, agent.KindCodex, review.Options{
		BinaryPath: bin,
		ExtraEnv:   []string{"FAKE_AGENT_SCENARIO=" + scenarioPath},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.OK {
		t.Fatalf("expected OK=false for a blocking finding, got %+v", result)
	}
	if len(result.PendingFindings) != 1 {
		t.Fatalf("expected the blocking finding to still be surfaced, got %+v", result.PendingFindings)
	}
	if !strings.Contains(result.Message, "hardcoded credential detected") {
		t.Fatalf("expected message to name the blocking finding, got %q", result.Message)
	}
}
