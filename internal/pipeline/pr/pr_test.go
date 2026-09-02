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
		RunID:       "run-123",
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

func TestRun_PRBodyCarriesPipelineAttestation(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "invocations.log")
	c := newClient(t, []string{"FAKE_GH_PR_URL=https://github.com/example/repo/pull/9"}, logPath)

	_, err := pr.Run(context.Background(), c, pr.Options{
		Title:       "made: automated change",
		Base:        "main",
		Head:        "made/run-321",
		EvidenceRef: "evidence/run-321/summary.txt",
		RunID:       "run-321",
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read invocation log: %v", err)
	}
	log := string(data)
	for _, want := range []string{"## Pipeline", "Run-ID: run-321", "Protocol-Version: "} {
		if !strings.Contains(log, want) {
			t.Fatalf("expected PR body to contain %q, log:\n%s", want, log)
		}
	}
}

func TestRun_PRBodyLinksEvidenceWhenURLKnown(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "invocations.log")
	c := newClient(t, []string{"FAKE_GH_PR_URL=https://github.com/example/repo/pull/10"}, logPath)

	_, err := pr.Run(context.Background(), c, pr.Options{
		Title:       "made: automated change",
		Base:        "main",
		Head:        "made/run-654",
		EvidenceRef: "made-evidence:run-654",
		EvidenceURL: "https://github.com/example/repo/tree/abc123/run-654",
		RunID:       "run-654",
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read invocation log: %v", err)
	}
	log := string(data)
	want := "[made-evidence:run-654](https://github.com/example/repo/tree/abc123/run-654)"
	if !strings.Contains(log, want) {
		t.Fatalf("expected PR body to link evidence as %q, log:\n%s", want, log)
	}
}

func TestRun_RejectsEmptyRunID(t *testing.T) {
	c := newClient(t, nil, "")

	_, err := pr.Run(context.Background(), c, pr.Options{
		Title:       "made: automated change",
		Base:        "main",
		Head:        "made/run-000",
		EvidenceRef: "evidence/run-000/summary.txt",
	})
	if err == nil {
		t.Fatal("expected an error when RunID is empty")
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
		RunID:       "run-456",
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

func TestRun_ReportsGHFailureAsInfrastructureError(t *testing.T) {
	c := newClient(t, []string{"FAKE_GH_EXIT_CODE=1", "FAKE_GH_STDERR=pr create failed: branch protection"}, "")

	_, err := pr.Run(context.Background(), c, pr.Options{
		Title:       "made: automated change",
		Base:        "main",
		Head:        "made/run-999",
		EvidenceRef: "evidence/run-999/summary.txt",
		RunID:       "run-999",
	})
	if err == nil {
		t.Fatal("expected GitHub API failure to be returned as infrastructure error")
	}
	if !strings.Contains(err.Error(), "GitHub API failure") {
		t.Fatalf("expected infrastructure classification, got %v", err)
	}
}
