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
	var authErr *github.AuthError
	if !errors.As(err, &authErr) {
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
	var authErr *github.AuthError
	if !errors.As(err, &authErr) {
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

func TestMergeableState_ParsesJSON(t *testing.T) {
	c := newClient(t, []string{`FAKE_GH_PR_VIEW_JSON={"mergeStateStatus":"BEHIND"}`}, "")

	state, err := c.MergeableState(context.Background(), "https://github.com/example/repo/pull/42")
	if err != nil {
		t.Fatalf("MergeableState: %v", err)
	}
	if state != "BEHIND" {
		t.Fatalf("expected BEHIND, got %q", state)
	}
}

func TestMergeableState_AuthFailurePreventsCall(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "invocations.log")
	c := newClient(t, []string{"FAKE_GH_AUTH_EXIT_CODE=1"}, logPath)

	_, err := c.MergeableState(context.Background(), "https://github.com/example/repo/pull/42")
	if err == nil {
		t.Fatal("expected an error when auth fails")
	}
	data, readErr := os.ReadFile(logPath)
	if readErr != nil {
		t.Fatalf("read invocation log: %v", readErr)
	}
	if strings.Contains(string(data), "pr view") {
		t.Fatalf("expected no pr view call after auth failure, log:\n%s", data)
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
