package pr_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/douglasjarquin/made/internal/github"
	"github.com/douglasjarquin/made/internal/pipeline/pr"
)

func TestRun_CreatePRIsIdempotentByRepositoryBaseAndHead(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "gh.log")
	statePath := filepath.Join(t.TempDir(), "created")
	bin := filepath.Join(t.TempDir(), "strict-gh")
	script := strings.Join([]string{
		"#!/bin/sh",
		"set -eu",
		"printf '%s\\n' \"$*\" >> \"$STRICT_GH_LOG\"",
		"if [ \"$1\" = auth ] && [ \"$2\" = status ]; then exit 0; fi",
		"if [ \"$1\" = pr ] && [ \"$2\" = list ]; then if [ -f \"$STRICT_GH_STATE\" ]; then printf '%s\\n' '[{\"url\":\"https://github.com/example/repo/pull/42\"}]'; else printf '%s\\n' '[]'; fi; exit 0; fi",
		"if [ \"$1\" = pr ] && [ \"$2\" = create ]; then touch \"$STRICT_GH_STATE\"; printf '%s\\n' 'https://github.com/example/repo/pull/42'; exit 0; fi",
		"exit 1",
		"",
	}, "\n")
	if err := os.WriteFile(bin, []byte(script), 0o700); err != nil {
		t.Fatalf("write strict gh fake: %v", err)
	}
	c := &github.Client{Binary: bin, Dir: t.TempDir(), ExtraEnv: []string{"STRICT_GH_LOG=" + logPath, "STRICT_GH_STATE=" + statePath}}
	opts := pr.Options{Title: "title", Base: "main", Head: "feature", EvidenceRef: "run-1", RunID: "run-1"}
	for i := 0; i < 2; i++ {
		result, err := pr.Run(context.Background(), c, opts)
		if err != nil || !result.OK {
			t.Fatalf("Run %d: result=%+v err=%v", i, result, err)
		}
	}
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read gh log: %v", err)
	}
	if count := strings.Count(string(data), "pr create"); count != 1 {
		t.Fatalf("expected one idempotent pr create call, got %d\n%s", count, data)
	}
}
