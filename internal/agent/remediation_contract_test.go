package agent_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/douglasjarquin/made/internal/agent"
	"github.com/douglasjarquin/made/internal/agent/agenttest"
)

func TestSpawn_CodexUsesSupportedExecStructuredContract(t *testing.T) {
	worktree := agentWorktree(t)
	head := strings.TrimSpace(gitAgent(t, worktree, "rev-parse", "HEAD"))
	t.Setenv("MADE_REVIEW_SECRET", "must-not-reach-agent")
	templateDir := t.TempDir()
	hooksDir := filepath.Join(templateDir, "hooks")
	if err := os.MkdirAll(hooksDir, 0o700); err != nil {
		t.Fatalf("mkdir Git template hooks: %v", err)
	}
	hookMarker := filepath.Join(t.TempDir(), "template-hook-fired")
	hookPath := filepath.Join(hooksDir, "post-checkout")
	hook := "#!/bin/sh\nprintf fired > " + shellQuote(hookMarker) + "\n"
	if err := os.WriteFile(hookPath, []byte(hook), 0o700); err != nil {
		t.Fatalf("write Git template hook: %v", err)
	}
	t.Setenv("GIT_TEMPLATE_DIR", templateDir)
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", "core.hooksPath")
	t.Setenv("GIT_CONFIG_VALUE_0", hooksDir)
	logPath := filepath.Join(t.TempDir(), "invocation.log")
	script := filepath.Join(t.TempDir(), "strict-codex")
	contents := strings.Join([]string{
		"#!/bin/sh",
		"set -eu",
		"printf '%s\\n' \"$@\" > \"$FAKE_AGENT_LOG_FILE\"",
		"[ \"$1\" = \"exec\" ]",
		"[ \"$2\" = \"--json\" ]",
		"[ \"$3\" = \"--output-schema\" ]",
		"[ -f \"$4\" ]",
		"[ \"$5\" = \"--output-last-message\" ]",
		"[ \"$7\" = \"--sandbox\" ]",
		"[ \"$8\" = \"read-only\" ]",
		"[ \"$9\" = \"--ephemeral\" ]",
		"[ \"${10}\" = \"-C\" ]",
		"[ -d \"${11}\" ]",
		"[ \"$(git -C \"${11}\" rev-parse HEAD)\" = " + shellQuote(head) + " ]",
		"if (umask 077; : > \"${11}/.agent-write-probe\") 2>/dev/null; then exit 1; fi",
		"test -z \"${MADE_REVIEW_SECRET:-}\"",
		"printf '%s\\n' '{\"findings\":[]}' > \"$6\"",
		"printf '%s\\n' '{\"findings\":[]}'",
		"",
	}, "\n")
	if err := os.WriteFile(script, []byte(contents), 0o700); err != nil {
		t.Fatalf("write strict Codex fake: %v", err)
	}

	findings, err := agent.Spawn(context.Background(), agent.KindCodex, agent.SpawnParams{
		WorktreePath: worktree,
		BinaryPath:   script,
		ExtraEnv: []string{
			"FAKE_AGENT_LOG_FILE=" + logPath,
		},
	})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	if len(findings.Findings) != 0 {
		t.Fatalf("expected empty structured findings, got %+v", findings)
	}
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read invocation log: %v", err)
	}
	args := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(args) < 3 {
		t.Fatalf("expected Codex invocation arguments, got %q", data)
	}
	if len(args) > 0 && args[0] == "review" {
		t.Fatalf("Codex invocation used obsolete review command: %s", data)
	}
	if _, err := os.Stat(args[10]); !os.IsNotExist(err) {
		t.Fatalf("review clone was not cleaned up: %v", err)
	}
	if _, err := os.Stat(hookMarker); !os.IsNotExist(err) {
		t.Fatalf("Git template or injected config hook ran during review setup: %v", err)
	}
}

func TestSpawn_RejectsReviewSymlinkThatEscapesClone(t *testing.T) {
	worktree := agentWorktree(t)
	outside := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outside, []byte("outside\n"), 0o600); err != nil {
		t.Fatalf("write outside target: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(worktree, "escape.txt")); err != nil {
		t.Fatalf("create escaping symlink: %v", err)
	}
	gitAgent(t, worktree, "add", "escape.txt")
	gitAgent(t, worktree, "commit", "-q", "-m", "add escaping symlink")
	scenarioPath := filepath.Join(t.TempDir(), "scenario.json")
	if err := os.WriteFile(scenarioPath, []byte(`{"findings":[]}`), 0o600); err != nil {
		t.Fatalf("write scenario: %v", err)
	}

	_, err := agent.Spawn(context.Background(), agent.KindCodex, agent.SpawnParams{
		WorktreePath: worktree,
		BinaryPath:   agenttest.Build(t),
		ExtraEnv:     []string{"FAKE_AGENT_KIND=codex", "FAKE_AGENT_SCENARIO=" + scenarioPath},
	})
	if err == nil || !strings.Contains(err.Error(), "escapes review worktree") {
		t.Fatalf("expected escaping review symlink rejection, got %v", err)
	}
}

func TestSpawn_ContainsReviewerFromSourceWorktree(t *testing.T) {
	worktree := agentWorktree(t)
	sourceFile := filepath.Join(worktree, "source.txt")
	if err := os.WriteFile(sourceFile, []byte("source-only\n"), 0o600); err != nil {
		t.Fatalf("write source fixture: %v", err)
	}
	gitAgent(t, worktree, "add", "source.txt")
	gitAgent(t, worktree, "commit", "-q", "-m", "add source fixture")
	script := filepath.Join(t.TempDir(), "containment-codex")
	contents := strings.Join([]string{
		"#!/bin/sh",
		"set -eu",
		"if cat " + shellQuote(filepath.Join(worktree, "source.txt")) + " >/dev/null 2>&1; then exit 41; fi",
		"if chmod -R u+w " + shellQuote(worktree) + " 2>/dev/null; then",
		"  if : > " + shellQuote(filepath.Join(worktree, "source-mutated")) + " 2>/dev/null; then exit 42; fi",
		"fi",
		"printf '%s\\n' '{\"findings\":[]}' > \"$6\"",
		"printf '%s\\n' '{\"findings\":[]}'",
		"",
	}, "\n")
	if err := os.WriteFile(script, []byte(contents), 0o700); err != nil {
		t.Fatalf("write containment Codex fake: %v", err)
	}

	findings, err := agent.Spawn(context.Background(), agent.KindCodex, agent.SpawnParams{
		WorktreePath: worktree,
		BinaryPath:   script,
		ExtraEnv:     nil,
	})
	if err != nil {
		t.Fatalf("Spawn should contain reviewer without changing source: %v", err)
	}
	if len(findings.Findings) != 0 {
		t.Fatalf("expected empty findings, got %+v", findings)
	}
	if _, err := os.Stat(filepath.Join(worktree, "source-mutated")); !os.IsNotExist(err) {
		t.Fatalf("reviewer modified source worktree: %v", err)
	}
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}
