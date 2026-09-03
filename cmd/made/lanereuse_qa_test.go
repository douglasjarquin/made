package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/douglasjarquin/made/internal/config"
	"github.com/douglasjarquin/made/internal/managed"
	"github.com/douglasjarquin/made/internal/planner"
	"github.com/douglasjarquin/made/internal/receipt"
)

// markerLaneConfig's Full command creates a file at markerPath, so whether
// the lane command actually executed is directly observable, not inferred
// from log messages.
func markerLaneConfig(markerPath string) string {
	return "version: 1\nvalidation:\n  lanes:\n    go:\n      paths: [\"**/*.go\"]\n      full: [\"touch " + markerPath + "\"]\n      required_before_push: true\n"
}

func newMarkerLaneSafetyRepo(t *testing.T, markerPath string) (dir, remote string) {
	t.Helper()
	dir = shortTempDir(t)
	remoteParent := shortTempDir(t)
	remote = filepath.Join(remoteParent, "remote.git")

	gitVerifyAt(t, remoteParent, "init", "-q", "--bare", remote)
	gitVerifyAt(t, dir, "init", "-b", "main")
	gitVerifyAt(t, dir, "config", "user.email", "test@test.local")
	gitVerifyAt(t, dir, "config", "user.name", "test")
	gitVerifyAt(t, dir, "remote", "add", "origin", remote)

	if err := os.WriteFile(filepath.Join(dir, ".made.yaml"), []byte(markerLaneConfig(markerPath)), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitVerifyAt(t, dir, "add", ".")
	gitVerifyAt(t, dir, "commit", "-m", "initial")
	baseSHA := gitVerifyAt(t, dir, "rev-parse", "HEAD")
	gitVerifyAt(t, dir, "update-ref", "refs/remotes/origin/main", baseSHA)

	if err := os.WriteFile(filepath.Join(dir, "hello.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitVerifyAt(t, dir, "add", ".")
	gitVerifyAt(t, dir, "commit", "-m", "add hello.go")

	return dir, remote
}

func publishMarkerLaneReceipt(t *testing.T, dir string, baseSHA, candidateSHA string) {
	t.Helper()
	configBytes, err := os.ReadFile(filepath.Join(dir, ".made.yaml"))
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	cfg, err := config.ParseBytes(configBytes)
	if err != nil {
		t.Fatalf("parse config: %v", err)
	}
	decisions, err := planner.SelectLanes(cfg.Validation.Lanes, []string{"hello.go"})
	if err != nil {
		t.Fatalf("SelectLanes: %v", err)
	}
	configHash, err := planner.HashConfig(cfg)
	if err != nil {
		t.Fatalf("HashConfig: %v", err)
	}
	var matchedPaths []string
	for _, d := range decisions {
		if d.Name == "go" {
			matchedPaths = d.MatchedPaths
		}
	}
	fp := receipt.BuildLaneFingerprint(receipt.LaneFingerprintInputs{
		RepoIdentity: receipt.RepoIdentity(context.Background(), dir),
		BaseSHA:      baseSHA,
		CandidateSHA: candidateSHA,
		ConfigHash:   configHash,
		LaneName:     "go",
		MatchedPaths: matchedPaths,
		Command:      cfg.Validation.Lanes["go"].FullShellCommands()[0],
		MadeVersion:  managed.MadeVersion,
	})
	store := &receipt.Store{RepoPath: dir}
	now := time.Now().UTC()
	if _, err := store.Put(context.Background(), fp.Hash(), receipt.Receipt{
		SchemaVersion: receipt.ReceiptSchemaVersion,
		Fingerprint:   fp,
		SourceRunID:   "prior-run",
		StartedAt:     now,
		CompletedAt:   now,
		MadeVersion:   managed.MadeVersion,
	}); err != nil {
		t.Fatalf("Put: %v", err)
	}
}

// TestVerifyRun_ReusedLaneCommandDoesNotActuallyExecute is the positive
// manual-QA case: a receipt matching this exact run already exists, so the
// lane's Full command (which would otherwise create a marker file) must be
// skipped - proven by the marker file's absence, not by log messages alone.
func TestVerifyRun_ReusedLaneCommandDoesNotActuallyExecute(t *testing.T) {
	markerDir := shortTempDir(t)
	marker := filepath.Join(markerDir, "lane-ran.marker")
	dir, _ := newMarkerLaneSafetyRepo(t, marker)

	baseSHA := gitVerifyAt(t, dir, "rev-parse", "origin/main")
	inputSHA := gitVerifyAt(t, dir, "rev-parse", "HEAD")
	publishMarkerLaneReceipt(t, dir, baseSHA, inputSHA)

	stdout, stderr, code := runCapture(t, []string{"verify", "run", "--repo", dir, "--base-ref", "origin/main", "--json"})
	if code != 0 {
		t.Fatalf("verify run: exit %d stderr=%s", code, stderr)
	}
	if !strings.Contains(string(stdout), `"go"`) || !strings.Contains(string(stdout), `"reused"`) {
		t.Fatalf("expected the go lane to be reported reused, got %s", stdout)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("expected the lane command to NOT execute (marker file must not exist), stat err=%v", err)
	}
}

// TestVerifyRun_FingerprintMismatchFallsBackToRealExecution is the negative
// manual-QA case: a receipt exists but for a different candidate SHA (a
// genuine fingerprint mismatch), so the lane command must actually run -
// proven by the marker file's presence.
func TestVerifyRun_FingerprintMismatchFallsBackToRealExecution(t *testing.T) {
	markerDir := shortTempDir(t)
	marker := filepath.Join(markerDir, "lane-ran.marker")
	dir, _ := newMarkerLaneSafetyRepo(t, marker)

	baseSHA := gitVerifyAt(t, dir, "rev-parse", "origin/main")
	staleSHA := "0000000000000000000000000000000000000f"
	publishMarkerLaneReceipt(t, dir, baseSHA, staleSHA)

	stdout, stderr, code := runCapture(t, []string{"verify", "run", "--repo", dir, "--base-ref", "origin/main", "--json"})
	if code != 0 {
		t.Fatalf("verify run: exit %d stderr=%s", code, stderr)
	}
	if strings.Contains(string(stdout), `"reused"`) {
		t.Fatalf("expected the mismatched receipt to be ignored, got %s", stdout)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("expected the lane command to actually execute and create the marker file: %v", err)
	}
}
