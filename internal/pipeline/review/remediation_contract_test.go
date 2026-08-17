package review_test

import (
	"os"
	"path/filepath"
	"strings"
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

func TestRun_AutoFixIgnoresAmbientGitRoutingAndHooks(t *testing.T) {
	bin := agenttest.Build(t)
	f := setupFixture(t)
	wt := f.addWorktree(t)
	t.Cleanup(func() { _ = wt.Remove() })
	patch := autoFixPatch(t, wt.Path)
	scenarioPath := writeScenario(t, agent.Findings{Findings: []agent.Finding{
		{Kind: agent.FindingAutoFixable, Description: "ignore ambient Git state", Patch: patch, Paths: []string{"reviewed.txt"}},
	}})

	alternateDir := t.TempDir()
	run(t, alternateDir, "init", "--bare", "-q")
	hooksDir := t.TempDir()
	hookMarker := filepath.Join(t.TempDir(), "hook-fired")
	if err := os.WriteFile(filepath.Join(hooksDir, "pre-commit"), []byte("#!/bin/sh\nprintf fired > '"+hookMarker+"'\n"), 0o700); err != nil {
		t.Fatalf("write auto-fix hook: %v", err)
	}
	t.Setenv("GIT_DIR", alternateDir)
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", "core.hooksPath")
	t.Setenv("GIT_CONFIG_VALUE_0", hooksDir)

	result, err := review.Run(t.Context(), wt.Path, agent.KindCodex, review.Options{
		BinaryPath: bin,
		ExtraEnv:   []string{"FAKE_AGENT_SCENARIO=" + scenarioPath},
	})
	if err != nil {
		t.Fatalf("review auto-fix inherited ambient Git routing/configuration: %v", err)
	}
	if !result.OK || len(result.AutoFixed) != 1 {
		t.Fatalf("expected one controlled auto-fix, got %+v", result)
	}
	for _, name := range []string{"GIT_DIR", "GIT_CONFIG_COUNT", "GIT_CONFIG_KEY_0", "GIT_CONFIG_VALUE_0"} {
		_ = os.Unsetenv(name)
	}
	if got := run(t, wt.Path, "status", "--porcelain"); got != "" {
		t.Fatalf("controlled auto-fix left worktree dirty: %q", got)
	}
	if _, err := os.Stat(hookMarker); !os.IsNotExist(err) {
		t.Fatalf("ambient auto-fix hook ran: %v", err)
	}
}

func TestRun_AutoFixSuppressesAmbientGitHook(t *testing.T) {
	bin := agenttest.Build(t)
	f := setupFixture(t)
	wt := f.addWorktree(t)
	t.Cleanup(func() { _ = wt.Remove() })
	patch := autoFixPatch(t, wt.Path)
	scenarioPath := writeScenario(t, agent.Findings{Findings: []agent.Finding{
		{Kind: agent.FindingAutoFixable, Description: "suppress ambient hook", Patch: patch, Paths: []string{"reviewed.txt"}},
	}})
	hooksDir := t.TempDir()
	hookMarker := filepath.Join(t.TempDir(), "hook-fired")
	if err := os.WriteFile(filepath.Join(hooksDir, "pre-commit"), []byte("#!/bin/sh\nprintf fired > '"+hookMarker+"'\n"), 0o700); err != nil {
		t.Fatalf("write auto-fix hook: %v", err)
	}
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", "core.hooksPath")
	t.Setenv("GIT_CONFIG_VALUE_0", hooksDir)

	result, err := review.Run(t.Context(), wt.Path, agent.KindCodex, review.Options{
		BinaryPath: bin,
		ExtraEnv:   []string{"FAKE_AGENT_SCENARIO=" + scenarioPath},
	})
	if err != nil {
		t.Fatalf("review auto-fix inherited ambient Git hook configuration: %v", err)
	}
	if !result.OK || len(result.AutoFixed) != 1 {
		t.Fatalf("expected one controlled auto-fix, got %+v", result)
	}
	if _, err := os.Stat(hookMarker); !os.IsNotExist(err) {
		t.Fatalf("ambient auto-fix hook ran: %v", err)
	}
}

func TestRun_RejectsDirectAgentWorktreeEdits(t *testing.T) {
	bin := agenttest.Build(t)
	f := setupFixture(t)
	wt := f.addWorktree(t)
	t.Cleanup(func() { _ = wt.Remove() })
	scenarioPath := writeScenario(t, agent.Findings{})

	_, err := review.Run(t.Context(), wt.Path, agent.KindCodex, review.Options{
		BinaryPath: bin,
		ExtraEnv: []string{
			"FAKE_AGENT_SCENARIO=" + scenarioPath,
			"FAKE_AGENT_WRITE_PATH=unreviewed.txt",
			"FAKE_AGENT_WRITE_DATA=agent must remain read-only",
		},
	})
	if err == nil {
		t.Fatal("review accepted direct agent edits to the worktree")
	}
	if _, statErr := os.Stat(filepath.Join(wt.Path, "unreviewed.txt")); !os.IsNotExist(statErr) {
		t.Fatalf("direct agent edit escaped review isolation: %v", statErr)
	}
	if got := run(t, wt.Path, "status", "--porcelain", "--untracked-files=all"); got != "" {
		t.Fatalf("direct agent edit left delivery worktree dirty: %q", got)
	}
}

func TestRun_AutoFixRejectsUnauthorizedDeletionBeforeApplyingPatch(t *testing.T) {
	bin := agenttest.Build(t)
	f := setupFixture(t)
	wt := f.addWorktree(t)
	t.Cleanup(func() { _ = wt.Remove() })

	writeFile(t, wt.Path, "unauthorized.txt", "must survive\n")
	run(t, wt.Path, "add", "unauthorized.txt")
	run(t, wt.Path, "commit", "-q", "-m", "seed unauthorized path")
	patch := deletionAndAllowedPatch(t, wt.Path)
	scenarioPath := writeScenario(t, agent.Findings{Findings: []agent.Finding{
		{Kind: agent.FindingAutoFixable, Description: "reject unauthorized deletion", Patch: patch, Paths: []string{"reviewed.txt"}},
	}})

	if _, err := review.Run(t.Context(), wt.Path, agent.KindCodex, review.Options{
		BinaryPath: bin,
		ExtraEnv:   []string{"FAKE_AGENT_SCENARIO=" + scenarioPath},
	}); err == nil {
		t.Fatal("review auto-fix accepted a patch deleting an unreturned tracked path")
	}
	if _, err := os.Stat(filepath.Join(wt.Path, "unauthorized.txt")); err != nil {
		t.Fatalf("unauthorized tracked file was removed before rejection: %v", err)
	}
	if got := run(t, wt.Path, "status", "--porcelain"); got != "" {
		t.Fatalf("rejected auto-fix left worktree dirty: %q", got)
	}
}

func TestRun_AutoFixRejectsForbiddenPatchHeaderBeforeApplyingPatch(t *testing.T) {
	bin := agenttest.Build(t)
	f := setupFixture(t)
	wt := f.addWorktree(t)
	t.Cleanup(func() { _ = wt.Remove() })

	patch := strings.Join([]string{
		"diff --git a/reviewed.txt b/../../unauthorized.txt",
		"--- a/reviewed.txt",
		"+++ b/../../unauthorized.txt",
		"@@ -1 +1 @@",
		"-line one",
		"+changed",
		"",
	}, "\n")
	scenarioPath := writeScenario(t, agent.Findings{Findings: []agent.Finding{
		{Kind: agent.FindingAutoFixable, Description: "reject forbidden header", Patch: patch, Paths: []string{"reviewed.txt"}},
	}})

	if _, err := review.Run(t.Context(), wt.Path, agent.KindCodex, review.Options{
		BinaryPath: bin,
		ExtraEnv:   []string{"FAKE_AGENT_SCENARIO=" + scenarioPath},
	}); err == nil {
		t.Fatal("review auto-fix accepted a forbidden patch header")
	}
	if got := run(t, wt.Path, "status", "--porcelain"); got != "" {
		t.Fatalf("forbidden auto-fix left worktree dirty: %q", got)
	}
}
