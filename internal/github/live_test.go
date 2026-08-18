package github_test

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/douglasjarquin/made/internal/github"
)

func TestLive_AuthStatusFailureWithInvalidToken(t *testing.T) {
	if os.Getenv("MADE_GITHUB_LIVE_TEST_REPO") == "" {
		t.Skip("set MADE_GITHUB_LIVE_TEST_REPO to run live gh tests")
	}

	c := &github.Client{
		Dir:      t.TempDir(),
		ExtraEnv: append(os.Environ(), "GH_TOKEN=invalid-token-for-made-task16-test"),
	}

	err := c.AuthStatus(context.Background())
	if err == nil {
		t.Fatal("expected AuthStatus to fail against real gh with an invalid GH_TOKEN")
	}
	var authErr *github.AuthError
	if !errors.As(err, &authErr) {
		t.Fatalf("expected *github.AuthError, got %T: %v", err, err)
	}
	t.Logf("real gh auth failure surfaced: %v", authErr)
}

func TestLive_AuthStatusAndPRCreation(t *testing.T) {
	repoDir := os.Getenv("MADE_GITHUB_LIVE_TEST_REPO")
	if repoDir == "" {
		t.Skip("set MADE_GITHUB_LIVE_TEST_REPO to a local clone of a disposable scratch repo to run this test against real gh/GitHub")
	}

	c := &github.Client{Dir: repoDir}

	if err := c.AuthStatus(context.Background()); err != nil {
		t.Fatalf("AuthStatus: %v", err)
	}

	base := os.Getenv("MADE_GITHUB_LIVE_TEST_BASE")
	if base == "" {
		base = "main"
	}
	head := os.Getenv("MADE_GITHUB_LIVE_TEST_HEAD")
	if head == "" {
		t.Fatal("set MADE_GITHUB_LIVE_TEST_HEAD to the pushed feature branch name")
	}

	url, err := c.CreatePR(context.Background(), github.CreatePROptions{
		Title: "made internal/github Task 16 live verification",
		Body:  "Opened by internal/github's live test to verify real PR creation. Safe to close/ignore.",
		Base:  base,
		Head:  head,
	})
	if err != nil {
		t.Fatalf("CreatePR: %v", err)
	}
	if !strings.HasPrefix(url, "https://github.com/") {
		t.Fatalf("expected a github.com PR URL, got %q", url)
	}
	t.Logf("created PR: %s", url)

	checks, err := c.PRChecks(context.Background(), url, github.CheckScopeAll)
	if err != nil {
		t.Fatalf("PRChecks: %v", err)
	}
	t.Logf("checks: %+v", checks)
}

func TestLive_CIWorkflowOwnershipSmoke(t *testing.T) {
	repoDir := os.Getenv("MADE_GITHUB_SMOKE_REPO")
	prURL := os.Getenv("MADE_GITHUB_SMOKE_PR_URL")
	if repoDir == "" || prURL == "" {
		t.Skip("set MADE_GITHUB_SMOKE_REPO and MADE_GITHUB_SMOKE_PR_URL for the disposable-repository smoke")
	}

	c := &github.Client{Dir: repoDir}
	if err := c.AuthStatus(context.Background()); err != nil {
		t.Fatalf("AuthStatus: %v", err)
	}
	checks, err := c.PRChecks(context.Background(), prURL, github.CheckScopeAll)
	if err != nil {
		t.Fatalf("PRChecks: %v", err)
	}
	if len(checks.Checks) == 0 {
		t.Fatal("expected the disposable PR to expose at least one check")
	}
	for _, check := range checks.Checks {
		if !check.InScope {
			t.Fatalf("all-scope check was not marked in scope: %+v", check)
		}
		if check.Rerunnable && (!check.ActionsBacked || check.WorkflowRunID == "") {
			t.Fatalf("rerunnable check lacks Actions ownership: %+v", check)
		}
		t.Logf("check name=%q required=%t actions_backed=%t rerunnable=%t run_id=%q link=%q", check.Name, check.Required, check.ActionsBacked, check.Rerunnable, check.WorkflowRunID, check.DetailsLink)
	}
}
