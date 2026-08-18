package ci_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/douglasjarquin/made/internal/github"
	"github.com/douglasjarquin/made/internal/github/githubtest"
	"github.com/douglasjarquin/made/internal/pipeline/ci"
)

func newClient(t *testing.T, extraEnv []string, logPath string) *github.Client {
	t.Helper()
	bin := githubtest.Build(t)
	env := append(os.Environ(), extraEnv...)
	if logPath != "" {
		env = append(env, "FAKE_GH_LOG_FILE="+logPath)
	}
	return &github.Client{
		Binary:   bin,
		Dir:      t.TempDir(),
		ExtraEnv: env,
	}
}

const testPollInterval = time.Millisecond

func TestRun_TransientFailureRecoversWithinBudget(t *testing.T) {
	stateDir := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "invocations.log")
	c := newClient(t, []string{
		"FAKE_GH_CHECKS_BUCKETS=fail,pass",
		"FAKE_GH_STATE_DIR=" + stateDir,
	}, logPath)

	result, err := ci.Run(context.Background(), c, "https://github.com/example/repo/pull/7", github.CheckScopeRequired, 2, testPollInterval)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !result.OK {
		t.Fatalf("expected OK=true, got %+v", result)
	}
	if result.RerunRoundsUsed != 1 {
		t.Fatalf("expected exactly one rerun round, got %d", result.RerunRoundsUsed)
	}

	data, readErr := os.ReadFile(logPath)
	if readErr != nil {
		t.Fatalf("read invocation log: %v", readErr)
	}
	rerunCount := strings.Count(string(data), "run rerun")
	if rerunCount != 1 {
		t.Fatalf("expected exactly one gh run rerun invocation, got %d, log:\n%s", rerunCount, data)
	}
}

func TestRun_BudgetExhaustionSurfacesFinalFailure(t *testing.T) {
	c := newClient(t, []string{
		"FAKE_GH_CHECKS_BUCKETS=fail",
		"FAKE_GH_RUN_LOG=build failed at step 3\n",
	}, "")

	const rerunBudget = 2
	result, err := ci.Run(context.Background(), c, "https://github.com/example/repo/pull/8", github.CheckScopeRequired, rerunBudget, testPollInterval)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.OK {
		t.Fatalf("expected OK=false, got %+v", result)
	}
	if result.RerunRoundsUsed != rerunBudget {
		t.Fatalf("expected RerunRoundsUsed == rerunBudget (%d), got %d", rerunBudget, result.RerunRoundsUsed)
	}
	if len(result.FailureEvidence) == 0 {
		t.Fatal("expected failure evidence on final failure")
	}
	if len(result.FailureEvidence) != 1 || !strings.Contains(result.FailureEvidence[0].Excerpt, "build failed at step 3") {
		t.Fatalf("expected failure evidence to contain the check's log output, got %+v", result.FailureEvidence)
	}
}

func TestRun_RejectsEmptyPRURL(t *testing.T) {
	c := newClient(t, nil, "")

	_, err := ci.Run(context.Background(), c, "", github.CheckScopeRequired, 2, testPollInterval)
	if err == nil {
		t.Fatal("expected an error when prURL is empty")
	}
}

func TestRun_RejectsNilClient(t *testing.T) {
	_, err := ci.Run(context.Background(), nil, "https://github.com/example/repo/pull/9", github.CheckScopeRequired, 2, testPollInterval)
	if err == nil {
		t.Fatal("expected an error when ghClient is nil")
	}
}

func TestRun_NeverExceedsBudgetEvenWithAlwaysFailingChecks(t *testing.T) {
	c := newClient(t, []string{
		"FAKE_GH_CHECKS_BUCKETS=fail",
	}, "")

	const rerunBudget = 3
	start := time.Now()
	result, err := ci.Run(context.Background(), c, "https://github.com/example/repo/pull/10", github.CheckScopeRequired, rerunBudget, testPollInterval)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.OK {
		t.Fatal("expected persistent failure to remain OK=false")
	}
	if result.RerunRoundsUsed != rerunBudget {
		t.Fatalf("expected exactly rerunBudget rounds (%d), got %d - budget was not respected", rerunBudget, result.RerunRoundsUsed)
	}
	if elapsed > 30*time.Second {
		t.Fatalf("Run took too long (%s) - suspect it looped past the budget", elapsed)
	}
}
