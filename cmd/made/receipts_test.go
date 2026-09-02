package main

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/douglasjarquin/made/internal/receipt"
)

func runGitForReceiptsCmdTest(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=receipts-cmd-test", "GIT_AUTHOR_EMAIL=receipts-cmd-test@example.com",
		"GIT_COMMITTER_NAME=receipts-cmd-test", "GIT_COMMITTER_EMAIL=receipts-cmd-test@example.com",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v in %s failed: %v: %s", args, dir, err, out)
	}
}

func initRepoForReceiptsCmdTest(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	runGitForReceiptsCmdTest(t, dir, "init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("write README.md: %v", err)
	}
	runGitForReceiptsCmdTest(t, dir, "add", "-A")
	runGitForReceiptsCmdTest(t, dir, "commit", "-q", "-m", "base commit")
	remote := filepath.Join(t.TempDir(), "remote.git")
	runGitForReceiptsCmdTest(t, t.TempDir(), "init", "-q", "--bare", remote)
	runGitForReceiptsCmdTest(t, dir, "remote", "add", "origin", remote)
	return dir
}

func TestReceiptsListCommand_JSONListsPublishedReceipts(t *testing.T) {
	dir := initRepoForReceiptsCmdTest(t)
	store := &receipt.Store{RepoPath: dir}
	fp := receipt.Fingerprint{SchemaVersion: 1, Lane: "go", ValidationLevel: "full", RepoIdentity: "x", BaseSHA: "a", CandidateSHA: "b", ConfigHash: "c", Command: []string{"echo", "hi"}, WorkingDirectory: ".", InputSetHash: "d", ToolchainHash: "e", OS: "linux", Arch: "amd64", MadeVersion: "dev", ProtocolVersion: 1}
	r := receipt.Receipt{SchemaVersion: 1, Fingerprint: fp, SourceRunID: "run-cli-test", StartedAt: time.Now(), CompletedAt: time.Now(), MadeVersion: "dev"}
	if _, err := store.Put(context.Background(), fp.Hash(), r); err != nil {
		t.Fatalf("Put: %v", err)
	}

	stdout, stderr, code := runCapture(t, []string{"receipts", "list", "--json", "--repo", dir})
	if code != 0 {
		t.Fatalf("expected exit 0, got %d, stderr:\n%s", code, stderr)
	}

	var report struct {
		Receipts []struct {
			SourceRunID string `json:"source_run_id"`
			Lane        string `json:"lane"`
		} `json:"receipts"`
	}
	if err := json.Unmarshal(stdout, &report); err != nil {
		t.Fatalf("decode receipts JSON: %v\noutput:\n%s", err, stdout)
	}
	if len(report.Receipts) != 1 || report.Receipts[0].SourceRunID != "run-cli-test" {
		t.Fatalf("expected 1 receipt naming run-cli-test, got %+v", report.Receipts)
	}
	if report.Receipts[0].Lane != "go" {
		t.Fatalf("expected lane %q, got %q", "go", report.Receipts[0].Lane)
	}
}

func TestReceiptsListCommand_HumanOutputOnEmptyRepo(t *testing.T) {
	dir := initRepoForReceiptsCmdTest(t)

	stdout, stderr, code := runCapture(t, []string{"receipts", "list", "--repo", dir})
	if code != 0 {
		t.Fatalf("expected exit 0, got %d, stderr:\n%s", code, stderr)
	}
	if len(stdout) == 0 {
		t.Fatal("expected some human-readable output even with no receipts")
	}
}
