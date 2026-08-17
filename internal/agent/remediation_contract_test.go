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
		"printf '%s\\n' \"$@\" > \"$STRICT_CODEX_LOG\"",
		"[ \"$1\" = \"exec\" ]",
		"[ \"$2\" = \"--cd\" ]",
		"[ \"$3\" != \"$STRICT_CODEX_WORKTREE\" ]",
		"[ -d \"$3\" ]",
		"[ \"$(git -C \"$3\" rev-parse HEAD)\" = \"$STRICT_CODEX_HEAD\" ]",
		"if (umask 077; : > \"$3/.agent-write-probe\") 2>/dev/null; then exit 1; fi",
		"shift 3",
		"has_json=0",
		"has_schema=0",
		"while [ \"$#\" -gt 0 ]; do",
		"  case \"$1\" in",
		"    --json) has_json=1 ;;",
		"    --output-schema) has_schema=1; shift; test -f \"$1\" ;;",
		"  esac",
		"  shift",
		"done",
		"[ \"$has_json\" -eq 1 ]",
		"[ \"$has_schema\" -eq 1 ]",
		"test -z \"${MADE_REVIEW_SECRET:-}\"",
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
			"STRICT_CODEX_LOG=" + logPath,
			"STRICT_CODEX_WORKTREE=" + worktree,
			"STRICT_CODEX_HEAD=" + head,
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
	if _, err := os.Stat(args[2]); !os.IsNotExist(err) {
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

	_, err := agent.Spawn(context.Background(), agent.KindClaude, agent.SpawnParams{
		WorktreePath: worktree,
		BinaryPath:   agenttest.Build(t),
		ExtraEnv:     []string{"FAKE_AGENT_SCENARIO=" + scenarioPath},
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
	marker := filepath.Join(t.TempDir(), "source-mutated")
	script := filepath.Join(t.TempDir(), "containment-codex")
	contents := strings.Join([]string{
		"#!/bin/sh",
		"set -eu",
		"if cat \"$STRICT_CODEX_SOURCE/source.txt\" >/dev/null 2>&1; then exit 41; fi",
		"if chmod -R u+w \"$STRICT_CODEX_SOURCE\" 2>/dev/null; then",
		"  if : > \"$STRICT_CODEX_SOURCE/source-mutated\" 2>/dev/null; then printf mutated > \"$STRICT_CODEX_MARKER\"; exit 42; fi",
		"fi",
		"printf '%s\\n' '{\"findings\":[]}'",
		"",
	}, "\n")
	if err := os.WriteFile(script, []byte(contents), 0o700); err != nil {
		t.Fatalf("write containment Codex fake: %v", err)
	}

	findings, err := agent.Spawn(context.Background(), agent.KindCodex, agent.SpawnParams{
		WorktreePath: worktree,
		BinaryPath:   script,
		ExtraEnv: []string{
			"STRICT_CODEX_SOURCE=" + worktree,
			"STRICT_CODEX_MARKER=" + marker,
		},
	})
	if err != nil {
		t.Fatalf("Spawn should contain reviewer without changing source: %v", err)
	}
	if len(findings.Findings) != 0 {
		t.Fatalf("expected empty findings, got %+v", findings)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("reviewer escaped into source worktree: %v", err)
	}
	if _, err := os.Stat(filepath.Join(worktree, "source-mutated")); !os.IsNotExist(err) {
		t.Fatalf("reviewer modified source worktree: %v", err)
	}
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}
