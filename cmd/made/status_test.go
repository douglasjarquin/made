package main

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/douglasjarquin/made/internal/daemon"
)

func TestStatusJSON_SchemaValidity(t *testing.T) {
	home := shortTempDir(t)
	t.Setenv("MADE_HOME", home)
	lockPath := filepath.Join(home, "daemon.lock")

	ctx, cancel := context.WithCancel(context.Background())
	ready := make(chan int, 1)
	rm, done := startDaemon(ctx, home, lockPath, time.Minute, func(pid int) { ready <- pid })

	select {
	case <-ready:
	case err := <-done:
		t.Fatalf("daemon exited before becoming ready: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("daemon did not become ready in time")
	}
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("daemon did not shut down after cancel")
		}
	})

	if _, err := rm.Submit("run-test-1", "example/repo", "feature-x", func(ctx context.Context, emit func(daemon.Event)) error {
		return nil
	}); err != nil {
		t.Fatalf("submit run: %v", err)
	}

	deadline := time.After(2 * time.Second)
	for {
		snap, ok := rm.Snapshot("run-test-1")
		if ok && (snap.Status == daemon.RunCompleted || snap.Status == daemon.RunFailed) {
			break
		}
		select {
		case <-deadline:
			t.Fatal("run did not reach a terminal state in time")
		case <-time.After(5 * time.Millisecond):
		}
	}

	out, errOut, code := runCapture(t, []string{"run", "status", "--json", "run-test-1"})
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stdout=%s stderr=%s", code, out, errOut)
	}

	var report StatusReport
	if err := json.Unmarshal(out, &report); err != nil {
		t.Fatalf("output is not valid JSON: %v\noutput: %s", err, out)
	}

	if report.SchemaVersion == 0 {
		t.Error("SchemaVersion must be set")
	}
	if report.RunID != "run-test-1" {
		t.Errorf("RunID = %q, want %q", report.RunID, "run-test-1")
	}
	if report.Repo != "example/repo" {
		t.Errorf("Repo = %q, want %q", report.Repo, "example/repo")
	}
	if report.Branch != "feature-x" {
		t.Errorf("Branch = %q, want %q", report.Branch, "feature-x")
	}
	switch report.State {
	case "queued", "running", "awaiting_review", "awaiting_merge", "succeeded", "failed", "canceled", "superseded":
	default:
		t.Errorf("State = %q, not one of the documented run states", report.State)
	}
	if len(report.Stages) == 0 {
		t.Error("Stages must be a non-empty per-stage result list")
	}
	seenStage := map[string]bool{}
	for _, s := range report.Stages {
		if s.Name == "" {
			t.Error("stage entry missing Name")
		}
		if seenStage[s.Name] {
			t.Errorf("stage %q reported more than once", s.Name)
		}
		seenStage[s.Name] = true
		switch s.Result {
		case "pass", "fail", "pending":
		default:
			t.Errorf("stage %q has unrecognized Result %q", s.Name, s.Result)
		}
	}
	if report.PendingFindings == nil {
		t.Error("PendingFindings must be present (an empty slice, not null)")
	}
}

func TestStatusJSON_ReflectsRealStageUpdate(t *testing.T) {
	home := shortTempDir(t)
	t.Setenv("MADE_HOME", home)
	lockPath := filepath.Join(home, "daemon.lock")

	ctx, cancel := context.WithCancel(context.Background())
	ready := make(chan int, 1)
	release := make(chan struct{})
	rm, done := startDaemon(ctx, home, lockPath, time.Minute, func(pid int) { ready <- pid })

	select {
	case <-ready:
	case err := <-done:
		t.Fatalf("daemon exited before becoming ready: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("daemon did not become ready in time")
	}
	t.Cleanup(func() {
		close(release)
		cancel()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("daemon did not shut down after cancel")
		}
	})

	if _, err := rm.Submit("run-real-stage-1", "example/repo", "feature-x", func(ctx context.Context, emit func(daemon.Event)) error {
		<-release
		return nil
	}); err != nil {
		t.Fatalf("submit run: %v", err)
	}

	wantStages := []daemon.StageResult{
		{Name: "intent", Result: "pass"},
		{Name: "rebase", Result: "pass"},
		{Name: "review", Result: "pending"},
	}
	if err := rm.UpdateStages("run-real-stage-1", wantStages); err != nil {
		t.Fatalf("UpdateStages: %v", err)
	}

	wantFindings := []daemon.AskUserFinding{
		{Stage: "review", Message: "Should this helper be exported?"},
	}
	if err := rm.UpdatePendingFindings("run-real-stage-1", wantFindings); err != nil {
		t.Fatalf("UpdatePendingFindings: %v", err)
	}

	out, errOut, code := runCapture(t, []string{"run", "status", "--json", "run-real-stage-1"})
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stdout=%s stderr=%s", code, out, errOut)
	}

	var report StatusReport
	if err := json.Unmarshal(out, &report); err != nil {
		t.Fatalf("output is not valid JSON: %v\noutput: %s", err, out)
	}

	if len(report.Stages) != len(wantStages) {
		t.Fatalf("Stages = %+v, want %+v", report.Stages, wantStages)
	}
	for i, want := range wantStages {
		if report.Stages[i] != StageResult(want) {
			t.Errorf("Stages[%d] = %+v, want %+v", i, report.Stages[i], want)
		}
	}

	if len(report.PendingFindings) != len(wantFindings) {
		t.Fatalf("PendingFindings = %+v, want %+v", report.PendingFindings, wantFindings)
	}
	for i, want := range wantFindings {
		if report.PendingFindings[i] != AskUserFinding(want) {
			t.Errorf("PendingFindings[%d] = %+v, want %+v", i, report.PendingFindings[i], want)
		}
	}
}

func TestStatus_NoRunsReportsError(t *testing.T) {
	home := shortTempDir(t)
	t.Setenv("MADE_HOME", home)
	lockPath := filepath.Join(home, "daemon.lock")

	ctx, cancel := context.WithCancel(context.Background())
	ready := make(chan int, 1)
	_, done := startDaemon(ctx, home, lockPath, time.Minute, func(pid int) { ready <- pid })
	select {
	case <-ready:
	case <-time.After(2 * time.Second):
		t.Fatal("daemon did not become ready in time")
	}
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("daemon did not shut down after cancel")
		}
	})

	_, _, code := runCapture(t, []string{"run", "status", "--json", "missing"})
	if code == 0 {
		t.Fatal("expected non-zero exit when no runs have been submitted")
	}
}

// runCapture runs the CLI's dispatch entrypoint in-process, capturing stdout
// and stderr through real *os.File pipes since run's signature is (args,
// stdout, stderr *os.File) rather than io.Writer.
func runCapture(t *testing.T, args []string) (stdout, stderr []byte, code int) {
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

	code = run(args, outW, errW)
	_ = outW.Close()
	_ = errW.Close()

	return <-outCh, <-errCh, code
}
