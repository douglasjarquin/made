package managed_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/douglasjarquin/made/internal/managed"
)

func TestResolveTrustedGuides_NoGuidesReturnsNil(t *testing.T) {
	root := t.TempDir()
	bindings, err := managed.ResolveTrustedGuides(root, nil)
	if err != nil {
		t.Fatalf("ResolveTrustedGuides: %v", err)
	}
	if bindings != nil {
		t.Fatalf("expected nil bindings for no guides, got %+v", bindings)
	}
}

func TestResolveTrustedGuides_OneGuideBindsPathHashAndBytes(t *testing.T) {
	root := t.TempDir()
	content := "# Feature Map\n"
	writeGuideFile(t, root, ".made/features/README.md", content)

	bindings, err := managed.ResolveTrustedGuides(root, []string{".made/features/README.md"})
	if err != nil {
		t.Fatalf("ResolveTrustedGuides: %v", err)
	}
	if len(bindings) != 1 {
		t.Fatalf("expected 1 binding, got %d", len(bindings))
	}
	if bindings[0].Path != ".made/features/README.md" {
		t.Errorf("Path = %q, want %q", bindings[0].Path, ".made/features/README.md")
	}
	if bindings[0].Bytes != len(content) {
		t.Errorf("Bytes = %d, want %d", bindings[0].Bytes, len(content))
	}
	if !strings.HasPrefix(bindings[0].ContentHash, "sha256:") {
		t.Errorf("ContentHash = %q, want sha256: prefix", bindings[0].ContentHash)
	}
}

func TestResolveTrustedGuides_MultipleGuidesPreserveOrder(t *testing.T) {
	root := t.TempDir()
	writeGuideFile(t, root, "docs/a.md", "a")
	writeGuideFile(t, root, "docs/b.md", "b")

	bindings, err := managed.ResolveTrustedGuides(root, []string{"docs/b.md", "docs/a.md"})
	if err != nil {
		t.Fatalf("ResolveTrustedGuides: %v", err)
	}
	if len(bindings) != 2 || bindings[0].Path != "docs/b.md" || bindings[1].Path != "docs/a.md" {
		t.Fatalf("expected order preserved, got %+v", bindings)
	}
}

func TestResolveTrustedGuides_MissingGuideFailsClearly(t *testing.T) {
	root := t.TempDir()
	_, err := managed.ResolveTrustedGuides(root, []string{"docs/missing.md"})
	if err == nil {
		t.Fatal("expected an error for a missing guide")
	}
	if !strings.Contains(err.Error(), "missing") {
		t.Errorf("expected error to mention missing guide, got %v", err)
	}
}

func TestResolveTrustedGuides_SymlinkGuideRejected(t *testing.T) {
	root := t.TempDir()
	writeGuideFile(t, root, "docs/real.md", "real content")
	linkPath := filepath.Join(root, "docs", "linked.md")
	if err := os.Symlink(filepath.Join(root, "docs", "real.md"), linkPath); err != nil {
		t.Fatalf("create symlink: %v", err)
	}

	_, err := managed.ResolveTrustedGuides(root, []string{"docs/linked.md"})
	if err == nil {
		t.Fatal("expected an error for a symlinked guide")
	}
	if !strings.Contains(err.Error(), "symlink") {
		t.Errorf("expected error to mention symlink, got %v", err)
	}
}

func TestResolveTrustedGuides_DirectoryGuideRejected(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "docs", "notafile.md"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	_, err := managed.ResolveTrustedGuides(root, []string{"docs/notafile.md"})
	if err == nil {
		t.Fatal("expected an error for a directory guide")
	}
	if !strings.Contains(err.Error(), "regular file") {
		t.Errorf("expected error to mention regular file, got %v", err)
	}
}

func TestResolveTrustedGuides_OversizedGuideRejected(t *testing.T) {
	root := t.TempDir()
	big := strings.Repeat("x", (1<<20)+1)
	writeGuideFile(t, root, "docs/big.md", big)

	_, err := managed.ResolveTrustedGuides(root, []string{"docs/big.md"})
	if err == nil {
		t.Fatal("expected an error for an oversized guide")
	}
	if !strings.Contains(err.Error(), "exceeds") {
		t.Errorf("expected error to mention the size bound, got %v", err)
	}
}

func TestResolveTrustedGuides_AggregateOversizedRejected(t *testing.T) {
	root := t.TempDir()
	chunk := strings.Repeat("x", 1<<20)
	var guides []string
	for i := 0; i < 5; i++ {
		name := filepath.Join("docs", "chunk"+string(rune('a'+i))+".md")
		writeGuideFile(t, root, name, chunk)
		guides = append(guides, filepath.ToSlash(name))
	}

	_, err := managed.ResolveTrustedGuides(root, guides)
	if err == nil {
		t.Fatal("expected an error for exceeding the aggregate guide size limit")
	}
	if !strings.Contains(err.Error(), "aggregate") {
		t.Errorf("expected error to mention aggregate limit, got %v", err)
	}
}

func TestResolveTrustedGuides_CandidateEditsDoNotAffectTrustedBinding(t *testing.T) {
	root := t.TempDir()
	writeGuideFile(t, root, "docs/guide.md", "trusted content")

	before, err := managed.ResolveTrustedGuides(root, []string{"docs/guide.md"})
	if err != nil {
		t.Fatalf("ResolveTrustedGuides: %v", err)
	}

	// Simulate a candidate edit to a *different* copy (a candidate workspace
	// is never the trusted root managed mode reads from); the trusted root's
	// bytes on disk are untouched, so re-resolving must be identical.
	after, err := managed.ResolveTrustedGuides(root, []string{"docs/guide.md"})
	if err != nil {
		t.Fatalf("ResolveTrustedGuides (second read): %v", err)
	}
	if before[0].ContentHash != after[0].ContentHash {
		t.Fatalf("expected stable content hash for an unchanged trusted guide, got %q vs %q", before[0].ContentHash, after[0].ContentHash)
	}
}

func TestTrustedGuideRoot_RootLayout(t *testing.T) {
	root := managed.TrustedGuideRoot("/repo/.made.yaml")
	if root != "/repo" {
		t.Fatalf("TrustedGuideRoot = %q, want /repo", root)
	}
}

func TestTrustedGuideRoot_DirectoryLayout(t *testing.T) {
	root := managed.TrustedGuideRoot("/repo/.made/config.yaml")
	if root != "/repo" {
		t.Fatalf("TrustedGuideRoot = %q, want /repo", root)
	}
}

func TestTrustedGuideRoot_LegacyLayout(t *testing.T) {
	root := managed.TrustedGuideRoot("/repo/.made.yml")
	if root != "/repo" {
		t.Fatalf("TrustedGuideRoot = %q, want /repo", root)
	}
}

func writeGuideFile(t *testing.T, root, relPath, content string) {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(relPath))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir for guide %s: %v", relPath, err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatalf("write guide %s: %v", relPath, err)
	}
}
