package ci_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/douglasjarquin/made/internal/github"
	"github.com/douglasjarquin/made/internal/github/githubtest"
	"github.com/douglasjarquin/made/internal/pipeline/ci"
)

func TestRun_UsesPrChecksJSONContract(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "gh.log")
	bin := githubtest.Build(t)
	c := &github.Client{
		Binary:   bin,
		Dir:      t.TempDir(),
		ExtraEnv: append(os.Environ(), "FAKE_GH_LOG_FILE="+logPath),
	}

	_, _ = ci.Run(context.Background(), c, "https://github.com/example/repo/pull/7", 0, 0)
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read invocation log: %v", err)
	}
	if !strings.Contains(string(data), "pr checks") {
		t.Fatalf("expected gh pr checks invocation, got %s", data)
	}
	if !strings.Contains(string(data), "name,state,bucket,link") {
		t.Fatalf("expected exact checks JSON fields, got %s", data)
	}
}

func TestRun_PassesWorkflowRunIDToLogsAndRerun(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "gh.log")
	bin := githubtest.Build(t)
	prURL := "https://github.com/example/repo/pull/8"
	c := &github.Client{
		Binary: bin,
		Dir:    t.TempDir(),
		ExtraEnv: append(os.Environ(),
			"FAKE_GH_LOG_FILE="+logPath,
			"FAKE_GH_CHECKS_JSON=[{\"name\":\"build\",\"state\":\"FAILURE\",\"bucket\":\"fail\",\"link\":\"https://github.com/example/repo/actions/runs/12345\"}]",
			"FAKE_GH_RUN_LOG=workflow failed\n",
		),
	}

	_, _ = ci.Run(context.Background(), c, prURL, 1, 0)
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read invocation log: %v", err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "invoked: args=run ") && strings.Contains(line, prURL) {
			t.Fatalf("PR URL was passed to a workflow-run command: %s", data)
		}
	}
	if !strings.Contains(string(data), "12345") {
		t.Fatalf("expected workflow run ID 12345 in run commands, got %s", data)
	}
}
