package receipt_test

import (
	"context"
	"os/exec"
	"testing"

	"github.com/douglasjarquin/made/internal/receipt"
)

func gitInitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", "-q", "-b", "main"},
		{"config", "user.email", "receipt-test@example.com"},
		{"config", "user.name", "receipt-test"},
	} {
		c := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	return dir
}

func TestRepoIdentity_ReturnsConfiguredOriginURL(t *testing.T) {
	dir := gitInitRepo(t)
	c := exec.Command("git", "-C", dir, "remote", "add", "origin", "https://example.com/repo.git")
	if out, err := c.CombinedOutput(); err != nil {
		t.Fatalf("git remote add: %v: %s", err, out)
	}

	got := receipt.RepoIdentity(context.Background(), dir)
	if got != "https://example.com/repo.git" {
		t.Fatalf("RepoIdentity = %q, want the configured origin URL", got)
	}
}

func TestRepoIdentity_ReturnsEmptyWithoutAnOriginRemote(t *testing.T) {
	dir := gitInitRepo(t)
	got := receipt.RepoIdentity(context.Background(), dir)
	if got != "" {
		t.Fatalf("RepoIdentity = %q, want empty string with no origin configured", got)
	}
}

func TestRepoIdentity_ReturnsEmptyForANonGitDirectory(t *testing.T) {
	dir := t.TempDir()
	got := receipt.RepoIdentity(context.Background(), dir)
	if got != "" {
		t.Fatalf("RepoIdentity = %q, want empty string for a non-git directory", got)
	}
}
