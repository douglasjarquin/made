package agent_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/douglasjarquin/made/internal/agent"
)

func TestSpawn_CodexUsesSupportedExecStructuredContract(t *testing.T) {
	worktree := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "invocation.log")
	script := filepath.Join(t.TempDir(), "strict-codex")
	contents := strings.Join([]string{
		"#!/bin/sh",
		"set -eu",
		"printf '%s\\n' \"$@\" > \"$STRICT_CODEX_LOG\"",
		"[ \"$1\" = \"exec\" ]",
		"[ \"$2\" = \"--cd\" ]",
		"[ \"$3\" = \"$STRICT_CODEX_WORKTREE\" ]",
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
	if strings.Contains(string(data), "review") {
		t.Fatalf("Codex invocation used obsolete review command: %s", data)
	}
}
