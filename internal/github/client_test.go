package github_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/douglasjarquin/made/internal/github"
	"github.com/douglasjarquin/made/internal/github/githubtest"
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

func TestAuthStatus_FailureReturnsAuthError(t *testing.T) {
	c := newClient(t, []string{
		"FAKE_GH_AUTH_EXIT_CODE=1",
		"FAKE_GH_AUTH_STDERR=You are not logged into any GitHub hosts.",
	}, "")

	err := c.AuthStatus(context.Background())
	if err == nil {
		t.Fatal("expected an error from AuthStatus")
	}
	authErr, ok := errors.AsType[*github.AuthError](err)
	if !ok {
		t.Fatalf("expected *github.AuthError, got %T: %v", err, err)
	}
	if !strings.Contains(authErr.Error(), "not logged into") {
		t.Fatalf("expected error to name the auth failure, got %q", authErr.Error())
	}
}

func TestAuthStatus_SuccessReturnsNil(t *testing.T) {
	c := newClient(t, nil, "")

	if err := c.AuthStatus(context.Background()); err != nil {
		t.Fatalf("expected AuthStatus to succeed, got %v", err)
	}
}

func TestCreatePR_AuthFailurePreventsPRCall(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "invocations.log")
	c := newClient(t, []string{"FAKE_GH_AUTH_EXIT_CODE=1"}, logPath)

	_, err := c.CreatePR(context.Background(), github.CreatePROptions{
		Title: "test PR",
		Body:  "body",
		Base:  "main",
		Head:  "feature",
	})
	if err == nil {
		t.Fatal("expected CreatePR to fail when auth fails")
	}
	_, ok := errors.AsType[*github.AuthError](err)
	if !ok {
		t.Fatalf("expected *github.AuthError, got %T: %v", err, err)
	}

	data, readErr := os.ReadFile(logPath)
	if readErr != nil {
		t.Fatalf("read invocation log: %v", readErr)
	}
	if strings.Contains(string(data), "pr create") {
		t.Fatalf("expected no pr create call after auth failure, log:\n%s", data)
	}
	if !strings.Contains(string(data), "auth status") {
		t.Fatalf("expected auth status to have been attempted, log:\n%s", data)
	}
}

func TestCreatePR_SuccessReturnsURL(t *testing.T) {
	c := newClient(t, []string{"FAKE_GH_PR_URL=https://github.com/example/repo/pull/42"}, "")

	url, err := c.CreatePR(context.Background(), github.CreatePROptions{
		Title: "test PR",
		Body:  "body",
		Base:  "main",
		Head:  "feature",
	})
	if err != nil {
		t.Fatalf("CreatePR: %v", err)
	}
	if url != "https://github.com/example/repo/pull/42" {
		t.Fatalf("expected PR URL, got %q", url)
	}
}

func TestPRChecks_ParsesJSON(t *testing.T) {
	c := newClient(t, []string{`FAKE_GH_CHECKS_JSON=[{"name":"build","state":"COMPLETED","bucket":"pass","link":"https://github.com/example/repo/actions/runs/42"}]`}, "")

	checks, err := c.PRChecks(context.Background(), "https://github.com/example/repo/pull/42", github.CheckScopeRequired)
	if err != nil {
		t.Fatalf("PRChecks: %v", err)
	}
	if checks.ExitCode != 0 || len(checks.Checks) != 1 {
		t.Fatalf("unexpected checks result: %+v", checks)
	}
	if checks.Checks[0].Bucket != "pass" || checks.Checks[0].WorkflowRunID != "42" {
		t.Fatalf("unexpected check fields: %+v", checks.Checks[0])
	}
}

func TestPRChecks_UsesRequiredScopeInvocation(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "invocations.log")
	c := newClient(t, nil, logPath)

	if _, err := c.PRChecks(context.Background(), "https://github.com/example/repo/pull/43", github.CheckScopeRequired); err != nil {
		t.Fatalf("PRChecks: %v", err)
	}
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read invocation log: %v", err)
	}
	if !strings.Contains(string(data), "pr checks") || !strings.Contains(string(data), "--required") {
		t.Fatalf("expected required-check invocation, log:\n%s", data)
	}
}

func TestPRChecks_AllAnnotatesRequiredAndActionOwnership(t *testing.T) {
	c := newClient(t, []string{
		`FAKE_GH_CHECKS_JSON=[{"name":"build","state":"SUCCESS","bucket":"pass","link":"https://github.com/example/repo/actions/runs/101"},{"name":"scanner","state":"FAILURE","bucket":"fail","link":"https://scanner.example/check/7"}]`,
		`FAKE_GH_REQUIRED_CHECKS_JSON=[{"name":"build","state":"SUCCESS","bucket":"pass","link":"https://github.com/example/repo/actions/runs/101"}]`,
	}, "")

	checks, err := c.PRChecks(context.Background(), "https://github.com/example/repo/pull/45", github.CheckScopeAll)
	if err != nil {
		t.Fatalf("PRChecks: %v", err)
	}
	if len(checks.Checks) != 2 {
		t.Fatalf("expected two checks, got %+v", checks.Checks)
	}
	if !checks.Checks[0].Required || !checks.Checks[0].InScope || !checks.Checks[0].ActionsBacked || !checks.Checks[0].Rerunnable || checks.Checks[0].WorkflowRunID != "101" {
		t.Fatalf("required Actions ownership was not modeled: %+v", checks.Checks[0])
	}
	if checks.Checks[1].Required || !checks.Checks[1].InScope || checks.Checks[1].ActionsBacked || checks.Checks[1].Rerunnable {
		t.Fatalf("external optional ownership was not modeled: %+v", checks.Checks[1])
	}
}

func TestPRChecks_MarksMalformedActionsLinkNonRerunnable(t *testing.T) {
	c := newClient(t, []string{
		`FAKE_GH_CHECKS_JSON=[{"name":"build","state":"FAILURE","bucket":"fail","link":"https://github.com/example/repo/actions/runs/not-a-number"}]`,
	}, "")

	checks, err := c.PRChecks(context.Background(), "https://github.com/example/repo/pull/46", github.CheckScopeRequired)
	if err != nil {
		t.Fatalf("PRChecks: %v", err)
	}
	if !checks.Checks[0].ActionsBacked || checks.Checks[0].Rerunnable || checks.Checks[0].WorkflowRunID != "" {
		t.Fatalf("malformed Actions link should be owned but not rerunnable: %+v", checks.Checks[0])
	}
}

func TestPRChecks_DoesNotTreatThirdPartyActionsPathAsOwned(t *testing.T) {
	c := newClient(t, []string{
		`FAKE_GH_CHECKS_JSON=[{"name":"scanner","state":"FAILURE","bucket":"fail","link":"https://scanner.example/actions/runs/99"}]`,
	}, "")

	checks, err := c.PRChecks(context.Background(), "https://github.com/example/repo/pull/49", github.CheckScopeRequired)
	if err != nil {
		t.Fatalf("PRChecks: %v", err)
	}
	if checks.Checks[0].ActionsBacked || checks.Checks[0].Rerunnable || checks.Checks[0].WorkflowRunID != "" {
		t.Fatalf("third-party Actions-shaped link should not be rerunnable: %+v", checks.Checks[0])
	}
}

func TestPRChecks_SurfacesAPIErrorWithJSONPayload(t *testing.T) {
	c := newClient(t, []string{
		`FAKE_GH_CHECKS_JSON=[{"name":"build","state":"SUCCESS","bucket":"pass","link":"https://github.com/example/repo/actions/runs/47"}]`,
		"FAKE_GH_CHECKS_EXIT_CODE=1",
		"FAKE_GH_STDERR=GitHub API unavailable",
	}, "")

	_, err := c.PRChecks(context.Background(), "https://github.com/example/repo/pull/47", github.CheckScopeRequired)
	if err == nil || !strings.Contains(err.Error(), "GitHub API unavailable") {
		t.Fatalf("expected API failure, got %v", err)
	}
}

func TestPRChecks_SurfacesRateLimitFailureWithJSONPayload(t *testing.T) {
	c := newClient(t, []string{
		`FAKE_GH_CHECKS_JSON=[{"name":"build","state":"SUCCESS","bucket":"pass","link":"https://github.com/example/repo/actions/runs/44"}]`,
		"FAKE_GH_CHECKS_EXIT_CODE=1",
		"FAKE_GH_STDERR=API rate limit exceeded",
	}, "")

	_, err := c.PRChecks(context.Background(), "https://github.com/example/repo/pull/44", github.CheckScopeRequired)
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "rate limit") {
		t.Fatalf("expected rate-limit error, got %v", err)
	}
}

func TestPRChecks_AllowsEmptyRequiredSet(t *testing.T) {
	c := newClient(t, []string{"FAKE_GH_CHECKS_JSON=[]"}, "")

	checks, err := c.PRChecks(context.Background(), "https://github.com/example/repo/pull/42", github.CheckScopeRequired)
	if err != nil {
		t.Fatalf("PRChecks rejected an empty required check set: %v", err)
	}
	if len(checks.Checks) != 0 {
		t.Fatalf("expected no applicable required checks, got %+v", checks.Checks)
	}
}

func TestMergeableState_AuthFailurePreventsCall(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "invocations.log")
	c := newClient(t, []string{"FAKE_GH_AUTH_EXIT_CODE=1"}, logPath)

	_, err := c.PRChecks(context.Background(), "https://github.com/example/repo/pull/42", github.CheckScopeRequired)
	if err == nil {
		t.Fatal("expected an error when auth fails")
	}
	data, readErr := os.ReadFile(logPath)
	if readErr != nil {
		t.Fatalf("read invocation log: %v", readErr)
	}
	if strings.Contains(string(data), "pr checks") {
		t.Fatalf("expected no pr checks call after auth failure, log:\n%s", data)
	}
}

func TestCheckLogs_ReturnsRunOutput(t *testing.T) {
	c := newClient(t, []string{"FAKE_GH_RUN_LOG=build failed at step 3\n"}, "")

	logs, err := c.CheckLogs(context.Background(), "123")
	if err != nil {
		t.Fatalf("CheckLogs: %v", err)
	}
	if logs != "build failed at step 3\n" {
		t.Fatalf("unexpected logs: %q", logs)
	}
}

func TestCheckLogs_AuthFailurePreventsCall(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "invocations.log")
	c := newClient(t, []string{"FAKE_GH_AUTH_EXIT_CODE=1"}, logPath)

	_, err := c.CheckLogs(context.Background(), "123")
	if err == nil {
		t.Fatal("expected an error when auth fails")
	}
	data, readErr := os.ReadFile(logPath)
	if readErr != nil {
		t.Fatalf("read invocation log: %v", readErr)
	}
	if strings.Contains(string(data), "run view") {
		t.Fatalf("expected no run view call after auth failure, log:\n%s", data)
	}
}

func TestRerunCheck_Success(t *testing.T) {
	c := newClient(t, nil, "")

	if err := c.RerunCheck(context.Background(), "123"); err != nil {
		t.Fatalf("RerunCheck: %v", err)
	}
}

func TestRerunCheck_AuthFailurePreventsCall(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "invocations.log")
	c := newClient(t, []string{"FAKE_GH_AUTH_EXIT_CODE=1"}, logPath)

	err := c.RerunCheck(context.Background(), "123")
	if err == nil {
		t.Fatal("expected an error when auth fails")
	}
	data, readErr := os.ReadFile(logPath)
	if readErr != nil {
		t.Fatalf("read invocation log: %v", readErr)
	}
	if strings.Contains(string(data), "run rerun") {
		t.Fatalf("expected no run rerun call after auth failure, log:\n%s", data)
	}
}

func TestRerunCheck_UnderlyingFailureSurfacesError(t *testing.T) {
	c := newClient(t, []string{"FAKE_GH_EXIT_CODE=1", "FAKE_GH_STDERR=run 123 not found"}, "")

	err := c.RerunCheck(context.Background(), "123")
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "run 123 not found") {
		t.Fatalf("expected error to surface gh stderr, got %v", err)
	}
}
