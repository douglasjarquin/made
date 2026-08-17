package github_test

import (
	"context"
	"strings"
	"testing"
)

func TestCheckLogs_RejectsPRURLWhenWorkflowRunIDIsRequired(t *testing.T) {
	c := newClient(t, []string{"FAKE_GH_RUN_LOG=secret log"}, "")

	_, err := c.CheckLogs(context.Background(), "https://github.com/example/repo/pull/42")
	if err == nil {
		t.Fatal("CheckLogs accepted a PR URL where a workflow run ID is required")
	}
	if !strings.Contains(err.Error(), "workflow") && !strings.Contains(err.Error(), "run") {
		t.Fatalf("error does not explain the workflow-run-ID boundary: %v", err)
	}
}
