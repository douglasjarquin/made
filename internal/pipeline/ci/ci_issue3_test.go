package ci_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/douglasjarquin/made/internal/github"
	"github.com/douglasjarquin/made/internal/pipeline/ci"
)

func TestRun_DoesNotRerunPendingChecks(t *testing.T) {
	stateDir := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "invocations.log")
	c := newClient(t, []string{
		"FAKE_GH_CHECKS_BUCKETS=pending,pass",
		"FAKE_GH_STATE_DIR=" + stateDir,
	}, logPath)

	result, err := ci.Run(context.Background(), c, "https://github.com/example/repo/pull/11", github.CheckScopeRequired, 2, testPollInterval)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !result.OK || result.RerunRoundsUsed != 0 {
		t.Fatalf("pending check was rerun: %+v", result)
	}
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read invocation log: %v", err)
	}
	if strings.Contains(string(data), "run rerun") {
		t.Fatalf("pending check triggered a rerun: %s", data)
	}
}

func TestRun_RerunsFailedActionsInsteadOfEarlierPassingActions(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "invocations.log")
	c := newClient(t, []string{
		`FAKE_GH_CHECKS_JSON=[{"name":"lint","state":"COMPLETED","bucket":"pass","link":"https://github.com/example/repo/actions/runs/101"},{"name":"test","state":"COMPLETED","bucket":"fail","link":"https://github.com/example/repo/actions/runs/202"}]`,
	}, logPath)

	_, err := ci.Run(context.Background(), c, "https://github.com/example/repo/pull/12", github.CheckScopeRequired, 1, testPollInterval)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read invocation log: %v", err)
	}
	if !strings.Contains(string(data), "run rerun 202") {
		t.Fatalf("expected failed test workflow 202 to be rerun, log:\n%s", data)
	}
	if strings.Contains(string(data), "run rerun 101") {
		t.Fatalf("passing lint workflow 101 was rerun, log:\n%s", data)
	}
}

func TestRun_RerunsUniqueFailedActionRunsInOneRound(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "invocations.log")
	c := newClient(t, []string{
		`FAKE_GH_CHECKS_JSON=[{"name":"build-linux","state":"FAILURE","bucket":"fail","link":"https://github.com/example/repo/actions/runs/201"},{"name":"build-macos","state":"FAILURE","bucket":"fail","link":"https://github.com/example/repo/actions/runs/201"},{"name":"deploy","state":"FAILURE","bucket":"fail","link":"https://github.com/example/repo/actions/runs/202"}]`,
	}, logPath)

	_, err := ci.Run(context.Background(), c, "https://github.com/example/repo/pull/13", github.CheckScopeRequired, 1, testPollInterval)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read invocation log: %v", err)
	}
	if strings.Count(string(data), "run rerun 201") != 1 || strings.Count(string(data), "run rerun 202") != 1 {
		t.Fatalf("expected one rerun for each unique failed workflow, log:\n%s", data)
	}
}

func TestRun_DoesNotRerunExternalFailureOrPassingActions(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "invocations.log")
	c := newClient(t, []string{
		`FAKE_GH_CHECKS_JSON=[{"name":"build","state":"SUCCESS","bucket":"pass","link":"https://github.com/example/repo/actions/runs/301"},{"name":"security-scan","state":"FAILURE","bucket":"fail","link":"https://scanner.example/check/44"}]`,
	}, logPath)

	result, err := ci.Run(context.Background(), c, "https://github.com/example/repo/pull/14", github.CheckScopeRequired, 1, testPollInterval)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.OK {
		t.Fatalf("expected external failure to remain failed: %+v", result)
	}
	if !strings.Contains(result.Message, "security-scan") || !strings.Contains(result.Message, "https://scanner.example/check/44") {
		t.Fatalf("failure message omitted external check identity: %q", result.Message)
	}

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read invocation log: %v", err)
	}
	if strings.Contains(string(data), "run rerun") {
		t.Fatalf("passing Actions or external check was rerun, log:\n%s", data)
	}
}

func TestRun_NoApplicableChecksIsClearFailure(t *testing.T) {
	c := newClient(t, []string{"FAKE_GH_CHECKS_JSON=[]"}, "")

	result, err := ci.Run(context.Background(), c, "https://github.com/example/repo/pull/15", github.CheckScopeRequired, 1, testPollInterval)
	if err != nil {
		t.Fatalf("Run returned infrastructure error for no applicable checks: %v", err)
	}
	if result.OK || !strings.Contains(strings.ToLower(result.Message), "no applicable") {
		t.Fatalf("expected clear no-applicable-check failure, got %+v", result)
	}
}

func TestRun_SkippedAndNeutralChecksAreTerminalSuccess(t *testing.T) {
	for _, tc := range []struct {
		name   string
		state  string
		bucket string
	}{
		{name: "skipped", state: "SKIPPED", bucket: "skipping"},
		{name: "neutral", state: "NEUTRAL", bucket: "neutral"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := newClient(t, []string{
				`FAKE_GH_CHECKS_JSON=[{"name":"optional-check","state":"` + tc.state + `","bucket":"` + tc.bucket + `","link":"https://scanner.example/check/55"}]`,
			}, "")

			result, err := ci.Run(context.Background(), c, "https://github.com/example/repo/pull/16", github.CheckScopeRequired, 0, testPollInterval)
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			if !result.OK || result.RerunRoundsUsed != 0 {
				t.Fatalf("expected %s check to be terminal success without rerun: %+v", tc.name, result)
			}
		})
	}
}

func TestRun_ExplicitTerminalFailureStatesNameTheCheck(t *testing.T) {
	for _, tc := range []struct {
		name  string
		state string
	}{
		{name: "canceled", state: "CANCELLED"},
		{name: "timed-out", state: "TIMED_OUT"},
		{name: "action-required", state: "ACTION_REQUIRED"},
		{name: "stale", state: "STALE"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := newClient(t, []string{
				`FAKE_GH_CHECKS_JSON=[{"name":"` + tc.name + `-check","state":"` + tc.state + `","bucket":"fail","link":"https://scanner.example/check/66"}]`,
			}, "")

			result, err := ci.Run(context.Background(), c, "https://github.com/example/repo/pull/17", github.CheckScopeRequired, 0, testPollInterval)
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			if result.OK || !strings.Contains(strings.ToLower(result.Message), tc.name) {
				t.Fatalf("terminal status was not surfaced by name: %+v", result)
			}
		})
	}
}

func TestRun_FailureEvidenceNamesCheckAndRun(t *testing.T) {
	c := newClient(t, []string{
		`FAKE_GH_CHECKS_JSON=[{"name":"build","state":"FAILURE","bucket":"fail","link":"https://github.com/example/repo/actions/runs/501"}]`,
		"FAKE_GH_RUN_LOG=build failed at step 3\n",
	}, "")

	result, err := ci.Run(context.Background(), c, "https://github.com/example/repo/pull/18", github.CheckScopeRequired, 0, testPollInterval)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.OK || !strings.Contains(result.Message, "build") || !strings.Contains(result.Message, "501") {
		t.Fatalf("failure message omitted check/run identity: %+v", result)
	}
	if len(result.FailureEvidence) != 1 || !strings.Contains(result.FailureEvidence[0].Excerpt, "build failed at step 3") {
		t.Fatalf("failure evidence omitted workflow output: %+v", result.FailureEvidence)
	}
}

func TestRun_FailureEvidenceFetchesEachFailedRunOnce(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "invocations.log")
	c := newClient(t, []string{
		`FAKE_GH_CHECKS_JSON=[{"name":"linux","state":"FAILURE","bucket":"fail","link":"https://github.com/example/repo/actions/runs/701"},{"name":"macos","state":"FAILURE","bucket":"fail","link":"https://github.com/example/repo/actions/runs/701"},{"name":"windows","state":"FAILURE","bucket":"fail","link":"https://github.com/example/repo/actions/runs/702"}]`,
		"FAKE_GH_RUN_LOG=workflow failed\n",
	}, logPath)

	result, err := ci.Run(context.Background(), c, "https://github.com/example/repo/pull/20", github.CheckScopeRequired, 0, testPollInterval)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.OK || !strings.Contains(result.Message, "linux, macos") || !strings.Contains(result.Message, "windows") {
		t.Fatalf("failure evidence did not associate names with runs: %+v", result)
	}

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read invocation log: %v", err)
	}
	if strings.Count(string(data), "run view 701") != 1 || strings.Count(string(data), "run view 702") != 1 {
		t.Fatalf("expected one bounded log fetch per failed run, log:\n%s", data)
	}
}

func TestRun_PendingThenFailureDoesNotConsumeRound(t *testing.T) {
	stateDir := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "invocations.log")
	c := newClient(t, []string{
		"FAKE_GH_CHECKS_BUCKETS=pending,fail",
		"FAKE_GH_STATE_DIR=" + stateDir,
	}, logPath)

	result, err := ci.Run(context.Background(), c, "https://github.com/example/repo/pull/21", github.CheckScopeRequired, 1, testPollInterval)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.RerunRoundsUsed != 1 {
		t.Fatalf("pending observation consumed rerun budget: %+v", result)
	}
}

func TestRun_CancellationDuringPendingStopsPolling(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()
	c := newClient(t, []string{
		`FAKE_GH_CHECKS_JSON=[{"name":"build","state":"PENDING","bucket":"pending","link":"https://github.com/example/repo/actions/runs/801"}]`,
	}, "")

	result, err := ci.Run(ctx, c, "https://github.com/example/repo/pull/22", github.CheckScopeRequired, 1, time.Second)
	if err != nil {
		t.Fatalf("Run returned infrastructure error during cancellation: %v", err)
	}
	if result.OK || !strings.Contains(result.Message, "deadline exceeded") || result.RerunRoundsUsed != 0 {
		t.Fatalf("cancellation did not stop pending polling cleanly: %+v", result)
	}
}

func TestRun_CachesSuccessfulAuthAcrossChecksRerunsAndLogs(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "invocations.log")
	c := newClient(t, []string{
		`FAKE_GH_CHECKS_JSON=[{"name":"build","state":"FAILURE","bucket":"fail","link":"https://github.com/example/repo/actions/runs/601"}]`,
		"FAKE_GH_RUN_LOG=build failed\n",
	}, logPath)

	_, err := ci.Run(context.Background(), c, "https://github.com/example/repo/pull/19", github.CheckScopeRequired, 1, testPollInterval)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read invocation log: %v", err)
	}
	if got := strings.Count(string(data), "auth status"); got != 1 {
		t.Fatalf("expected one successful auth preflight across the run, got %d, log:\n%s", got, data)
	}
}
