package pr_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/douglasjarquin/made/internal/github"
	"github.com/douglasjarquin/made/internal/github/githubtest"
	"github.com/douglasjarquin/made/internal/pipeline/pr"
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

func TestRun_OpensPRWithEvidenceLink(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "invocations.log")
	c := newClient(t, []string{"FAKE_GH_PR_URL=https://github.com/example/repo/pull/7"}, logPath)

	result, err := pr.Run(context.Background(), c, pr.Options{
		Title:       "made: automated change",
		Base:        "main",
		Head:        "made/run-123",
		EvidenceRef: "evidence/run-123/summary.txt",
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !result.OK {
		t.Fatalf("expected OK=true, got %+v", result)
	}
	if result.PRURL != "https://github.com/example/repo/pull/7" {
		t.Fatalf("expected PR URL, got %q", result.PRURL)
	}

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read invocation log: %v", err)
	}
	if !strings.Contains(string(data), "evidence/run-123/summary.txt") {
		t.Fatalf("expected PR body to reference the run's evidence, log:\n%s", data)
	}
}

func TestRun_NeverInvokesMergeCommand(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "invocations.log")
	c := newClient(t, []string{"FAKE_GH_PR_URL=https://github.com/example/repo/pull/8"}, logPath)

	result, err := pr.Run(context.Background(), c, pr.Options{
		Title:       "made: automated change",
		Base:        "main",
		Head:        "made/run-456",
		EvidenceRef: "evidence/run-456/summary.txt",
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !result.OK {
		t.Fatalf("expected OK=true, got %+v", result)
	}

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read invocation log: %v", err)
	}
	if strings.Contains(strings.ToLower(string(data)), "merge") {
		t.Fatalf("expected no merge-related gh invocation ever, log:\n%s", data)
	}
}

func TestRun_RejectsEmptyEvidenceRef(t *testing.T) {
	c := newClient(t, nil, "")

	_, err := pr.Run(context.Background(), c, pr.Options{
		Title: "made: automated change",
		Base:  "main",
		Head:  "made/run-789",
	})
	if err == nil {
		t.Fatal("expected an error when EvidenceRef is empty")
	}
}

func TestRun_ReportsGHFailureAsResultNotError(t *testing.T) {
	c := newClient(t, []string{"FAKE_GH_EXIT_CODE=1", "FAKE_GH_STDERR=pr create failed: branch protection"}, "")

	result, err := pr.Run(context.Background(), c, pr.Options{
		Title:       "made: automated change",
		Base:        "main",
		Head:        "made/run-999",
		EvidenceRef: "evidence/run-999/summary.txt",
	})
	if err != nil {
		t.Fatalf("Run: expected a reported failure, not an error, got: %v", err)
	}
	if result.OK {
		t.Fatalf("expected OK=false, got %+v", result)
	}
	if result.Message == "" {
		t.Fatal("expected a non-empty failure message")
	}
}
