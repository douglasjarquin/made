package ci_test

import (
	"context"
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
	if result.OK || len(result.Message) > 256*1024 || !strings.Contains(result.Message, "[truncated]") {
		t.Fatalf("failure evidence was not bounded: len=%d message suffix=%q", len(result.Message), messageTail)
	}
}
