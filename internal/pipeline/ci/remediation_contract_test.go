package ci_test

import (
	"context"
	"testing"
	"time"

	"github.com/douglasjarquin/made/internal/github"
	"github.com/douglasjarquin/made/internal/pipeline/ci"
)

func TestRun_AuthenticationFailureIsInfrastructureError(t *testing.T) {
	c := newClient(t, []string{
		"FAKE_GH_AUTH_EXIT_CODE=1",
		"FAKE_GH_AUTH_STDERR=not authenticated",
	}, "")

	result, err := ci.Run(context.Background(), c, "https://github.com/example/repo/pull/42", github.CheckScopeRequired, 0, time.Millisecond)
	if err == nil {
		t.Fatalf("authentication failure was reported as a failed check: result=%+v", result)
	}
}
