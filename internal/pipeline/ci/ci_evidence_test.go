package ci_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/douglasjarquin/made/internal/github"
	"github.com/douglasjarquin/made/internal/pipeline/ci"
)

func TestRun_BoundsAggregatedFailureEvidence(t *testing.T) {
	c := newClient(t, []string{
		`FAKE_GH_CHECKS_JSON=[{"name":"build","state":"FAILURE","bucket":"fail","link":"https://github.com/example/repo/actions/runs/901"}]`,
		"FAKE_GH_RUN_LOG=" + strings.Repeat("x", 128*1024),
	}, "")

	result, err := ci.Run(context.Background(), c, "https://github.com/example/repo/pull/23", github.CheckScopeRequired, 0, testPollInterval)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	messageTail := result.Message
	if len(messageTail) > 32 {
		messageTail = messageTail[len(messageTail)-32:]
	}
	if result.OK || len(result.FailureEvidence) != 1 || len(result.FailureEvidence[0].Excerpt) > 64*1024+len("\n[truncated]\n") || !strings.Contains(result.FailureEvidence[0].Excerpt, "[truncated]") || strings.Contains(result.Message, "[truncated]") {
		t.Fatalf("failure evidence was not bounded outside the durable message: len=%d message suffix=%q evidence=%+v", len(result.Message), messageTail, result.FailureEvidence)
	}
}

func TestRun_BoundsFailureLogFetchesAcrossRuns(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "invocations.log")
	checks := make([]string, 0, 5)
	for runID := 901; runID <= 905; runID++ {
		checks = append(checks, fmt.Sprintf(`{"name":"build-%d","state":"FAILURE","bucket":"fail","link":"https://github.com/example/repo/actions/runs/%d"}`, runID, runID))
	}
	c := newClient(t, []string{
		"FAKE_GH_CHECKS_JSON=[" + strings.Join(checks, ",") + "]",
		"FAKE_GH_RUN_LOG=workflow failed\n",
	}, logPath)

	result, err := ci.Run(context.Background(), c, "https://github.com/example/repo/pull/24", github.CheckScopeRequired, 0, testPollInterval)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read invocation log: %v", err)
	}
	if got := strings.Count(string(data), "run view "); got != 4 {
		t.Fatalf("expected at most four bounded log fetches, got %d, log:\n%s", got, data)
	}
	if len(result.FailureEvidence) != 5 || !strings.Contains(result.Message, "905") || !strings.Contains(result.FailureEvidence[4].Excerpt, "[log excerpt omitted after evidence limit]") {
		t.Fatalf("bounded evidence omitted the skipped run identity or marker: message=%q evidence=%+v", result.Message, result.FailureEvidence)
	}
}

func TestRun_RedactsSecretsFromFailureEvidence(t *testing.T) {
	c := newClient(t, []string{
		`FAKE_GH_CHECKS_JSON=[{"name":"build","state":"FAILURE","bucket":"fail","link":"https://github.com/example/repo/actions/runs/906"}]`,
		"FAKE_GH_RUN_LOG=token=workflow-secret\n",
	}, "")

	result, err := ci.Run(context.Background(), c, "https://github.com/example/repo/pull/25", github.CheckScopeRequired, 0, testPollInterval)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if strings.Contains(result.Message, "workflow-secret") || strings.Contains(result.FailureEvidence[0].Excerpt, "workflow-secret") || !strings.Contains(result.FailureEvidence[0].Excerpt, "token=[REDACTED]") {
		t.Fatalf("failure evidence did not redact the secret: message=%q evidence=%+v", result.Message, result.FailureEvidence)
	}
}
