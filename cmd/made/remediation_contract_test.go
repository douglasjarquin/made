package main

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/douglasjarquin/made/internal/daemon"
)

func TestRun_CapabilitiesJSONIsVersionedAndListsStructuredCommands(t *testing.T) {
	code, stdout, stderr := captureRun(t, "capabilities", "--json")
	if code != 0 {
		t.Fatalf("capabilities exit=%d stderr=%q", code, stderr)
	}
	var payload struct {
		SchemaVersion   int      `json:"schema_version"`
		ProtocolVersion int      `json:"protocol_version"`
		Commands        []string `json:"commands"`
	}
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		t.Fatalf("decode capabilities JSON: %v; stdout=%q", err, stdout)
	}
	if payload.SchemaVersion != 1 || payload.ProtocolVersion != 1 {
		t.Fatalf("capability versions = schema %d protocol %d, want 1/1", payload.SchemaVersion, payload.ProtocolVersion)
	}
	for _, want := range []string{"run.submit", "run.status", "run.list", "run.cancel", "review.decide", "doctor"} {
		if !containsString(payload.Commands, want) {
			t.Fatalf("capabilities missing structured command %q: %v", want, payload.Commands)
		}
	}
}

func TestEnsureMadeHome_RepairsGroupAndOtherPermissions(t *testing.T) {
	home := filepath.Join(t.TempDir(), "made")
	if err := os.Mkdir(home, 0o755); err != nil {
		t.Fatalf("create made home: %v", err)
	}
	if _, err := ensureMadeHome(home); err != nil {
		t.Fatalf("ensureMadeHome: %v", err)
	}
	info, err := os.Stat(home)
	if err != nil {
		t.Fatalf("stat made home: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o700 {
		t.Fatalf("made home permissions = %o, want 700", got)
	}
}

func TestEnsureMadeHome_RejectsSymlink(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatalf("create target: %v", err)
	}
	link := filepath.Join(root, "made")
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("create made home symlink: %v", err)
	}
	if _, err := ensureMadeHome(link); err == nil {
		t.Fatal("ensureMadeHome accepted a symlink")
	}
}

func TestRun_SubmitJSONReturnsExactRunIDAndImmutableInputHead(t *testing.T) {
	home := shortTempDir(t)
	t.Setenv("MADE_HOME", home)
	ctx, cancel := context.WithCancel(context.Background())
	ready := make(chan int, 1)
	_, done := startDaemon(ctx, home, filepath.Join(home, "daemon.lock"), time.Hour, func(pid int) { ready <- pid })
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("daemon cleanup: %v", err)
			}
		case <-time.After(2 * time.Second):
			t.Error("daemon did not stop during cleanup")
		}
	})
	select {
	case <-ready:
	case err := <-done:
		t.Fatalf("daemon stopped before submit: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("daemon did not become ready")
	}

	inputSHA := strings.Repeat("a", 40)
	code, stdout, stderr := captureRun(t, "run", "submit", "--json", "--repo", "/repo/example", "--branch", "feature", "--input-sha", inputSHA)
	if code != 0 {
		t.Fatalf("run submit exit=%d stderr=%q", code, stderr)
	}
	var payload struct {
		RunID    string `json:"run_id"`
		State    string `json:"state"`
		InputSHA string `json:"input_sha"`
	}
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		t.Fatalf("decode submit JSON: %v; stdout=%q", err, stdout)
	}
	if payload.RunID == "" || payload.State != "queued" || payload.InputSHA != inputSHA {
		t.Fatalf("submit payload = %+v, want exact queued run identity", payload)
	}
}

func TestRunSubmit_RejectsInvalidOutputSHA(t *testing.T) {
	rm := daemon.NewRunManager()
	_, err := runSubmitHandler(rm)(context.Background(), []byte(`{"repo":"/repo","branch":"feature","input_sha":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","output_sha":"not-a-sha"}`))
	if err == nil {
		t.Fatal("run.submit accepted an invalid output_sha")
	}
}

func TestStatusHandler_RequiresExactRunID(t *testing.T) {
	rm := daemon.NewRunManager()
	started := make(chan struct{})
	id := rm.NewRunID()
	if _, err := rm.Submit(id, "repo", "branch", func(ctx context.Context, _ func(daemon.Event)) error {
		close(started)
		<-ctx.Done()
		return ctx.Err()
	}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	<-started
	t.Cleanup(func() { _ = rm.Cancel(id) })

	_, err := statusHandler(rm)(context.Background(), nil)
	if err == nil {
		t.Fatal("status handler resolved a global latest run without an exact run ID")
	}
}

func TestStatusReport_JSONHasFixedDurableRunSchema(t *testing.T) {
	raw, err := json.Marshal(newStatusReport(daemon.RunSnapshot{
		ID:     "123e4567-e89b-12d3-a456-426614174000",
		Repo:   "/repo",
		Branch: "feature",
		Status: daemon.RunStatus("awaiting_merge"),
	}))
	if err != nil {
		t.Fatalf("marshal status report: %v", err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatalf("decode status report: %v", err)
	}
	for _, field := range []string{
		"schema_version", "protocol_version", "run_id", "repo", "branch", "state",
		"input_sha", "output_sha", "execution_finished", "findings", "decisions",
		"pr_url", "errors", "superseded_by", "cancel_requested", "submission_events",
	} {
		if _, ok := fields[field]; !ok {
			t.Fatalf("status schema missing durable field %q: %s", field, raw)
		}
	}
}

func TestReviewDecide_RejectsUnknownExactRunID(t *testing.T) {
	store := daemon.NewReviewDecisions()
	_, err := reviewDecideRunHandler(daemon.NewRunManager(), store)(context.Background(), []byte(`{"run_id":"missing","stage":"review","decision":"approved"}`))
	if err == nil {
		t.Fatal("review.decide accepted a decision for an unknown exact run ID")
	}
}

func TestRun_ListJSONExposesBatchActiveRunQuery(t *testing.T) {
	home := shortTempDir(t)
	t.Setenv("MADE_HOME", home)
	ctx, cancel := context.WithCancel(context.Background())
	ready := make(chan int, 1)
	_, done := startDaemon(ctx, home, filepath.Join(home, "daemon.lock"), time.Hour, func(pid int) { ready <- pid })
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Error("daemon did not stop during cleanup")
		}
	})
	select {
	case <-ready:
	case err := <-done:
		t.Fatalf("daemon stopped before list: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("daemon did not become ready")
	}

	code, stdout, stderr := captureRun(t, "run", "list", "--json", "--active")
	if code != 0 {
		t.Fatalf("run list exit=%d stderr=%q", code, stderr)
	}
	var payload struct {
		SchemaVersion int `json:"schema_version"`
		Runs          []struct {
			RunID string `json:"run_id"`
		} `json:"runs"`
	}
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		t.Fatalf("decode run list JSON: %v; stdout=%q", err, stdout)
	}
	if payload.SchemaVersion != 1 || payload.Runs == nil {
		t.Fatalf("run list payload = %+v, want versioned active batch", payload)
	}
}

func captureRun(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	outFile, err := os.CreateTemp(t.TempDir(), "stdout-")
	if err != nil {
		t.Fatalf("create stdout fixture: %v", err)
	}
	errFile, err := os.CreateTemp(t.TempDir(), "stderr-")
	if err != nil {
		t.Fatalf("create stderr fixture: %v", err)
	}
	code := run(args, outFile, errFile)
	if _, err := outFile.Seek(0, io.SeekStart); err != nil {
		t.Fatalf("seek stdout fixture: %v", err)
	}
	stdout, err := io.ReadAll(outFile)
	if err != nil {
		t.Fatalf("read stdout fixture: %v", err)
	}
	if _, err := errFile.Seek(0, io.SeekStart); err != nil {
		t.Fatalf("seek stderr fixture: %v", err)
	}
	stderr, err := io.ReadAll(errFile)
	if err != nil {
		t.Fatalf("read stderr fixture: %v", err)
	}
	return code, string(stdout), string(stderr)
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
