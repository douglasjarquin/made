package receipt_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/douglasjarquin/made/internal/receipt"
)

func runGitForReceiptTest(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=receipt-test", "GIT_AUTHOR_EMAIL=receipt-test@example.com",
		"GIT_COMMITTER_NAME=receipt-test", "GIT_COMMITTER_EMAIL=receipt-test@example.com",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v in %s failed: %v: %s", args, dir, err, out)
	}
}

func initRepoWithOrigin(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	runGitForReceiptTest(t, dir, "init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("write README.md: %v", err)
	}
	runGitForReceiptTest(t, dir, "add", "-A")
	runGitForReceiptTest(t, dir, "commit", "-q", "-m", "base commit")

	remote := filepath.Join(t.TempDir(), "remote.git")
	runGitForReceiptTest(t, t.TempDir(), "init", "--bare", "-q", remote)
	runGitForReceiptTest(t, dir, "remote", "add", "origin", remote)
	return dir
}

func testReceipt() receipt.Receipt {
	return receipt.Receipt{
		SchemaVersion: receipt.ReceiptSchemaVersion,
		Fingerprint:   baseFingerprint(),
		SourceRunID:   "run-abc",
		StartedAt:     time.Unix(1000, 0).UTC(),
		CompletedAt:   time.Unix(1010, 0).UTC(),
		MadeVersion:   "dev",
	}
}

func TestStore_PutThenGetRoundTrips(t *testing.T) {
	dir := initRepoWithOrigin(t)
	store := &receipt.Store{RepoPath: dir}
	want := testReceipt()

	if _, err := store.Put(context.Background(), want.Fingerprint.Hash(), want); err != nil {
		t.Fatalf("Put: %v", err)
	}

	got, ok, reason := store.Get(context.Background(), want.Fingerprint.Hash())
	if !ok {
		t.Fatalf("expected Get to find the published receipt, reason: %s", reason)
	}
	if got.SourceRunID != want.SourceRunID {
		t.Fatalf("expected SourceRunID %q, got %q", want.SourceRunID, got.SourceRunID)
	}
	if got.Fingerprint.Hash() != want.Fingerprint.Hash() {
		t.Fatalf("expected round-tripped fingerprint to match")
	}
}

func TestStore_GetReturnsNotFoundForUnknownFingerprint(t *testing.T) {
	dir := initRepoWithOrigin(t)
	store := &receipt.Store{RepoPath: dir}

	_, ok, reason := store.Get(context.Background(), "sha256:doesnotexist")
	if ok {
		t.Fatal("expected ok=false for a fingerprint that was never published")
	}
	if reason == "" {
		t.Fatal("expected a non-empty reason")
	}
}

func TestStore_GetFailsOpenOnEmptyRepo(t *testing.T) {
	dir := t.TempDir()
	runGitForReceiptTest(t, dir, "init", "-q", "-b", "main")
	store := &receipt.Store{RepoPath: dir}

	_, ok, reason := store.Get(context.Background(), "sha256:anything")
	if ok {
		t.Fatal("expected ok=false when the receipts branch does not exist yet")
	}
	if reason == "" {
		t.Fatal("expected a non-empty reason")
	}
}

func TestStore_DistinguishesFingerprints(t *testing.T) {
	dir := initRepoWithOrigin(t)
	store := &receipt.Store{RepoPath: dir}
	a := testReceipt()
	b := testReceipt()
	b.Fingerprint.Lane = "docs"
	b.SourceRunID = "run-xyz"

	if _, err := store.Put(context.Background(), a.Fingerprint.Hash(), a); err != nil {
		t.Fatalf("Put a: %v", err)
	}
	if _, err := store.Put(context.Background(), b.Fingerprint.Hash(), b); err != nil {
		t.Fatalf("Put b: %v", err)
	}

	gotA, ok, _ := store.Get(context.Background(), a.Fingerprint.Hash())
	if !ok || gotA.SourceRunID != "run-abc" {
		t.Fatalf("expected receipt a to round-trip independently, got %+v ok=%v", gotA, ok)
	}
	gotB, ok, _ := store.Get(context.Background(), b.Fingerprint.Hash())
	if !ok || gotB.SourceRunID != "run-xyz" {
		t.Fatalf("expected receipt b to round-trip independently, got %+v ok=%v", gotB, ok)
	}
}
