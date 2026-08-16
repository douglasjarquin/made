package main

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/douglasjarquin/made/internal/api"
)

// startReviewTestServer fakes only the "status" handler's PendingFindings
// (real runs never populate that field yet, per status.go) while wiring the
// real review.decide/review.decision handlers, so the round trip under test
// is genuine except for the one field no orchestrator produces yet.
func startReviewTestServer(t *testing.T, fixture StatusReport) string {
	t.Helper()

	home := shortTempDir(t)
	socketPath := api.SocketPath(home)

	srv := api.NewServer(socketPath)
	srv.Handle("status", func(ctx context.Context, params json.RawMessage) (any, error) {
		return fixture, nil
	})
	store := newReviewDecisions()
	store.RegisterRun(fixture.RunID)
	srv.Handle("review.decide", reviewDecideHandler(store))
	srv.Handle("review.decision", reviewDecisionHandler(store))

	if err := srv.Listen(); err != nil {
		t.Fatalf("Listen: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- srv.Serve(ctx) }()
	t.Cleanup(func() {
		cancel()
		<-done
		_ = srv.Close()
	})

	t.Setenv("MADE_HOME", home)
	return home
}

func queryDecision(t *testing.T, home, runID, stage string) (string, bool) {
	t.Helper()
	client, err := api.Dial(api.SocketPath(home))
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer func() { _ = client.Close() }()

	var result reviewDecisionResult
	if err := client.CallInto("review.decision", reviewDecisionParams{RunID: runID, Stage: stage}, &result); err != nil {
		t.Fatalf("review.decision: %v", err)
	}
	return result.Decision, result.Found
}

func runReviewCapture(t *testing.T, args []string, stdin string) (stdout, stderr []byte, code int) {
	t.Helper()

	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	errR, errW, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}

	outCh := make(chan []byte, 1)
	errCh := make(chan []byte, 1)
	go func() {
		b, _ := io.ReadAll(outR)
		outCh <- b
	}()
	go func() {
		b, _ := io.ReadAll(errR)
		errCh <- b
	}()

	code = runReviewCommand(args, strings.NewReader(stdin), outW, errW)
	_ = outW.Close()
	_ = errW.Close()

	return <-outCh, <-errCh, code
}

func TestReview_ApproveResumes(t *testing.T) {
	fixture := StatusReport{
		SchemaVersion: statusSchemaVersion,
		RunID:         "run-approve-1",
		Repo:          "example/repo",
		Branch:        "feature-x",
		State:         "running",
		Stages:        []StageResult{{Name: "review", Result: StageResultPending}},
		PendingFindings: []AskUserFinding{
			{Stage: "review", Message: "Should this helper be exported?"},
		},
	}
	home := startReviewTestServer(t, fixture)

	out, errOut, code := runReviewCapture(t, []string{"--run", "run-approve-1"}, "a\n")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stdout=%s stderr=%s", code, out, errOut)
	}
	if !strings.Contains(string(out), "review") || !strings.Contains(string(out), "Should this helper be exported?") {
		t.Errorf("stdout missing finding text: %s", out)
	}

	decision, found := queryDecision(t, home, "run-approve-1", "review")
	if !found {
		t.Fatal("decision not recorded")
	}
	if decision != ReviewApproved {
		t.Errorf("decision = %q, want %q", decision, ReviewApproved)
	}
}

func TestReview_RejectHalts(t *testing.T) {
	fixture := StatusReport{
		SchemaVersion: statusSchemaVersion,
		RunID:         "run-reject-1",
		Repo:          "example/repo",
		Branch:        "feature-x",
		State:         "running",
		Stages:        []StageResult{{Name: "document", Result: StageResultPending}},
		PendingFindings: []AskUserFinding{
			{Stage: "document", Message: "Does this doc change need a changelog entry?"},
		},
	}
	home := startReviewTestServer(t, fixture)

	out, errOut, code := runReviewCapture(t, []string{"--run", "run-reject-1"}, "r\n")
	if code == 0 {
		t.Fatalf("exit code = %d, want non-zero on rejection; stdout=%s stderr=%s", code, out, errOut)
	}

	decision, found := queryDecision(t, home, "run-reject-1", "document")
	if !found {
		t.Fatal("decision not recorded")
	}
	if decision != ReviewRejected {
		t.Errorf("decision = %q, want %q", decision, ReviewRejected)
	}
}

func TestReview_NoPendingFindings(t *testing.T) {
	fixture := StatusReport{
		SchemaVersion:   statusSchemaVersion,
		RunID:           "run-clean-1",
		Repo:            "example/repo",
		Branch:          "feature-x",
		State:           "completed",
		Stages:          []StageResult{{Name: "review", Result: StageResultPass}},
		PendingFindings: []AskUserFinding{},
	}
	startReviewTestServer(t, fixture)

	out, errOut, code := runReviewCapture(t, []string{"--run", "run-clean-1"}, "")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stdout=%s stderr=%s", code, out, errOut)
	}
	if !strings.Contains(string(out), "no pending findings") {
		t.Errorf("stdout = %s, want mention of no pending findings", out)
	}
}
