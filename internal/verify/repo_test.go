package verify_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/douglasjarquin/made/internal/verify"
)

func TestResolveContext_RootLayout(t *testing.T) {
	dir, baseSHA, inputSHA := newTestRepo(t, ".made.yaml", testConfigNoAgent)

	rc, err := verify.ResolveContext(context.Background(), dir, "origin/main")
	if err != nil {
		t.Fatalf("ResolveContext: %v", err)
	}
	if rc.InputSHA != inputSHA {
		t.Errorf("InputSHA = %q, want %q", rc.InputSHA, inputSHA)
	}
	if rc.BaseSHA != baseSHA {
		t.Errorf("BaseSHA = %q, want %q", rc.BaseSHA, baseSHA)
	}
	if !strings.HasSuffix(rc.Config.Path, ".made.yaml") {
		t.Errorf("Config.Path = %q, want suffix .made.yaml", rc.Config.Path)
	}
	if rc.Warning != "" {
		t.Errorf("unexpected warning: %q", rc.Warning)
	}
}

func TestResolveContext_DirectoryLayoutEquivalent(t *testing.T) {
	dir, baseSHA, inputSHA := newTestRepo(t, filepath.Join(".made", "config.yaml"), testConfigNoAgent)

	rc, err := verify.ResolveContext(context.Background(), dir, "origin/main")
	if err != nil {
		t.Fatalf("ResolveContext: %v", err)
	}
	if rc.InputSHA != inputSHA || rc.BaseSHA != baseSHA {
		t.Errorf("identity mismatch: got base=%s input=%s", rc.BaseSHA, rc.InputSHA)
	}
	if !strings.HasSuffix(rc.Config.Path, filepath.Join(".made", "config.yaml")) {
		t.Errorf("Config.Path = %q, want directory layout", rc.Config.Path)
	}
}

func TestResolveContext_AbsentConfigSynthesizesEmpty(t *testing.T) {
	dir, _, _ := newTestRepo(t, "", "")

	rc, err := verify.ResolveContext(context.Background(), dir, "origin/main")
	if err != nil {
		t.Fatalf("ResolveContext: %v", err)
	}
	if rc.Warning == "" {
		t.Error("expected a warning for absent configuration")
	}
	if string(rc.ConfigBytes) != "version: 1\n" {
		t.Errorf("ConfigBytes = %q, want synthetic empty config", rc.ConfigBytes)
	}
	if strings.HasPrefix(rc.Config.Path, dir) {
		t.Errorf("synthetic config path %q must not live inside the repository", rc.Config.Path)
	}
}

func TestResolveContext_LegacyConfigWarns(t *testing.T) {
	dir, _, _ := newTestRepo(t, ".made.yml", testConfigNoAgent)

	rc, err := verify.ResolveContext(context.Background(), dir, "origin/main")
	if err != nil {
		t.Fatalf("ResolveContext: %v", err)
	}
	if rc.Warning == "" {
		t.Error("expected a legacy-config deprecation warning")
	}
}

func TestResolveContext_ConflictingConfigsFail(t *testing.T) {
	dir, _, _ := newTestRepo(t, ".made.yaml", testConfigNoAgent)
	writeTestFile(t, filepath.Join(dir, ".made", "config.yaml"), testConfigNoAgent)
	gitAt(t, dir, "add", ".")
	gitAt(t, dir, "commit", "-m", "add conflicting config")

	_, err := verify.ResolveContext(context.Background(), dir, "origin/main")
	if err == nil {
		t.Fatal("expected an error for conflicting config layouts")
	}
}

func TestResolveContext_DirtyWorktreeFails(t *testing.T) {
	dir, _, _ := newTestRepo(t, ".made.yaml", testConfigNoAgent)
	writeTestFile(t, filepath.Join(dir, "dirty.txt"), "uncommitted\n")

	_, err := verify.ResolveContext(context.Background(), dir, "origin/main")
	if err == nil || !strings.Contains(err.Error(), "not clean") {
		t.Fatalf("expected a dirty-worktree error, got %v", err)
	}
}

func TestResolveContext_StagedChangesFail(t *testing.T) {
	dir, _, _ := newTestRepo(t, ".made.yaml", testConfigNoAgent)
	writeTestFile(t, filepath.Join(dir, "staged.txt"), "staged\n")
	gitAt(t, dir, "add", "staged.txt")

	_, err := verify.ResolveContext(context.Background(), dir, "origin/main")
	if err == nil || !strings.Contains(err.Error(), "not clean") {
		t.Fatalf("expected a dirty-worktree error for staged changes, got %v", err)
	}
}

func TestResolveContext_MissingBaseRefFails(t *testing.T) {
	dir, _, _ := newTestRepo(t, ".made.yaml", testConfigNoAgent)

	_, err := verify.ResolveContext(context.Background(), dir, "origin/does-not-exist")
	if err == nil || !strings.Contains(err.Error(), "never fetches automatically") {
		t.Fatalf("expected a local-only base-ref error, got %v", err)
	}
}

func TestResolveContext_EmptyBaseRefFails(t *testing.T) {
	dir, _, _ := newTestRepo(t, ".made.yaml", testConfigNoAgent)

	_, err := verify.ResolveContext(context.Background(), dir, "")
	if err == nil {
		t.Fatal("expected an error for an empty --base-ref")
	}
}

func TestResolveContext_DivergedBranchStillResolvesMergeBase(t *testing.T) {
	dir, baseSHA, _ := newTestRepo(t, ".made.yaml", testConfigNoAgent)

	// Diverge origin/main from HEAD's ancestry: a commit on the base ref that
	// input_sha never incorporates. merge-base must still resolve to baseSHA.
	gitAt(t, dir, "checkout", "-b", "diverged", baseSHA)
	writeTestFile(t, filepath.Join(dir, "diverged.txt"), "diverged\n")
	gitAt(t, dir, "add", ".")
	gitAt(t, dir, "commit", "-m", "diverged commit")
	divergedSHA := gitAt(t, dir, "rev-parse", "HEAD")
	gitAt(t, dir, "update-ref", "refs/remotes/origin/main", divergedSHA)
	gitAt(t, dir, "checkout", "main")

	rc, err := verify.ResolveContext(context.Background(), dir, "origin/main")
	if err != nil {
		t.Fatalf("ResolveContext: %v", err)
	}
	if rc.BaseSHA != baseSHA {
		t.Errorf("BaseSHA = %q, want merge-base %q", rc.BaseSHA, baseSHA)
	}
}

func TestResolveContext_Guides(t *testing.T) {
	cfg := "version: 1\nreview:\n  guides:\n    - GUIDE.md\ncommands:\n  test: \"true\"\n"
	dir, _, _ := newTestRepo(t, ".made.yaml", cfg)
	writeTestFile(t, filepath.Join(dir, "GUIDE.md"), "be careful\n")
	gitAt(t, dir, "add", ".")
	gitAt(t, dir, "commit", "-m", "add guide")

	rc, err := verify.ResolveContext(context.Background(), dir, "origin/main")
	if err != nil {
		t.Fatalf("ResolveContext: %v", err)
	}
	if len(rc.Guides) != 1 || rc.Guides[0].Path != "GUIDE.md" {
		t.Fatalf("Guides = %+v, want one binding for GUIDE.md", rc.Guides)
	}
	firstHash := rc.Guides[0].ContentHash

	writeTestFile(t, filepath.Join(dir, "GUIDE.md"), "be very careful\n")
	gitAt(t, dir, "add", ".")
	gitAt(t, dir, "commit", "-m", "edit guide")

	rc2, err := verify.ResolveContext(context.Background(), dir, "origin/main")
	if err != nil {
		t.Fatalf("ResolveContext after edit: %v", err)
	}
	if rc2.Guides[0].ContentHash == firstHash {
		t.Error("expected the guide's content hash to change after editing it")
	}
}

func TestResolveContext_NoCommitsFails(t *testing.T) {
	dir := t.TempDir()
	gitAt(t, dir, "init", "-b", "main")

	_, err := verify.ResolveContext(context.Background(), dir, "origin/main")
	if err == nil {
		t.Fatal("expected an error for a repository with no commits")
	}
}

func TestResolveContext_NotAGitRepoFails(t *testing.T) {
	dir := t.TempDir()
	_, err := verify.ResolveContext(context.Background(), dir, "origin/main")
	if err == nil {
		t.Fatal("expected an error for a non-Git directory")
	}
}

func TestMain(m *testing.M) {
	os.Exit(m.Run())
}
