package cursor_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/douglasjarquin/made/internal/config"
	"github.com/douglasjarquin/made/internal/cursor"
)

func cfgWithCursor(model string, guides ...string) config.Config {
	return config.Config{
		Review: config.Review{
			Guides: guides,
			Executors: config.ReviewExecutors{
				Cursor: config.CursorExecutor{Model: model},
			},
		},
	}
}

func TestInit_FreshRepoCreatesBothProjections(t *testing.T) {
	root := t.TempDir()
	results, err := cursor.Init(root, cfgWithCursor("gpt-5"), false)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d: %+v", len(results), results)
	}
	for _, r := range results {
		if r.Action != cursor.ActionCreated {
			t.Fatalf("expected created action for %s, got %s", r.RelPath, r.Action)
		}
	}
	assertFileContainsMarker(t, filepath.Join(root, cursor.SkillRelPath))
	assertFileContainsMarker(t, filepath.Join(root, cursor.ReviewerRelPath))
}

func TestInit_NoCursorModelOnlyCreatesSkill(t *testing.T) {
	root := t.TempDir()
	results, err := cursor.Init(root, config.Config{}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].RelPath != cursor.SkillRelPath {
		t.Fatalf("expected only the skill file to be created, got %+v", results)
	}
	if _, err := os.Stat(filepath.Join(root, cursor.ReviewerRelPath)); !os.IsNotExist(err) {
		t.Fatalf("expected no reviewer file without a configured cursor model")
	}
}

func TestInit_IsIdempotent(t *testing.T) {
	root := t.TempDir()
	if _, err := cursor.Init(root, cfgWithCursor("gpt-5"), false); err != nil {
		t.Fatal(err)
	}
	results, err := cursor.Init(root, cfgWithCursor("gpt-5"), false)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range results {
		if r.Action != cursor.ActionUnchanged {
			t.Fatalf("expected unchanged on second init, got %s for %s", r.Action, r.RelPath)
		}
	}
}

func TestSync_IsDeterministicByteIdentical(t *testing.T) {
	root := t.TempDir()
	cfg := cfgWithCursor("gpt-5", "docs/guide.md")
	if _, err := cursor.Sync(root, cfg, false); err != nil {
		t.Fatal(err)
	}
	first, err := os.ReadFile(filepath.Join(root, cursor.ReviewerRelPath))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cursor.Sync(root, cfg, false); err != nil {
		t.Fatal(err)
	}
	second, err := os.ReadFile(filepath.Join(root, cursor.ReviewerRelPath))
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Fatal("expected byte-identical output across syncs with unchanged config")
	}
}

func TestSync_ModelChangeUpdatesOnlyReviewer(t *testing.T) {
	root := t.TempDir()
	if _, err := cursor.Sync(root, cfgWithCursor("gpt-5"), false); err != nil {
		t.Fatal(err)
	}
	skillBefore, err := os.ReadFile(filepath.Join(root, cursor.SkillRelPath))
	if err != nil {
		t.Fatal(err)
	}

	results, err := cursor.Sync(root, cfgWithCursor("claude-opus-5"), false)
	if err != nil {
		t.Fatal(err)
	}

	var reviewerUpdated, skillUpdated bool
	for _, r := range results {
		if r.RelPath == cursor.ReviewerRelPath && r.Action == cursor.ActionUpdated {
			reviewerUpdated = true
		}
		if r.RelPath == cursor.SkillRelPath && r.Action != cursor.ActionUnchanged {
			skillUpdated = true
		}
	}
	if !reviewerUpdated {
		t.Fatalf("expected reviewer to update on model change, got %+v", results)
	}
	if skillUpdated {
		t.Fatal("expected skill projection to be unaffected by a model change")
	}
	skillAfter, err := os.ReadFile(filepath.Join(root, cursor.SkillRelPath))
	if err != nil {
		t.Fatal(err)
	}
	if string(skillBefore) != string(skillAfter) {
		t.Fatal("expected skill file bytes to be unchanged by a model change")
	}
}

func TestSync_GuideChangeUpdatesOnlyReviewer(t *testing.T) {
	root := t.TempDir()
	if _, err := cursor.Sync(root, cfgWithCursor("gpt-5"), false); err != nil {
		t.Fatal(err)
	}
	skillBefore, _ := os.ReadFile(filepath.Join(root, cursor.SkillRelPath))

	results, err := cursor.Sync(root, cfgWithCursor("gpt-5", "docs/new-guide.md"), false)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range results {
		if r.RelPath == cursor.SkillRelPath && r.Action != cursor.ActionUnchanged {
			t.Fatalf("expected skill unaffected by guide change, got %s", r.Action)
		}
	}
	skillAfter, _ := os.ReadFile(filepath.Join(root, cursor.SkillRelPath))
	if string(skillBefore) != string(skillAfter) {
		t.Fatal("expected skill file bytes unaffected by a guide change")
	}
	reviewer, err := os.ReadFile(filepath.Join(root, cursor.ReviewerRelPath))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(reviewer), "docs/new-guide.md") {
		t.Fatalf("expected reviewer to reference the new guide path, got:\n%s", reviewer)
	}
}

func TestSync_ModelRemovedRemovesOwnedReviewer(t *testing.T) {
	root := t.TempDir()
	if _, err := cursor.Sync(root, cfgWithCursor("gpt-5"), false); err != nil {
		t.Fatal(err)
	}
	results, err := cursor.Sync(root, config.Config{}, false)
	if err != nil {
		t.Fatal(err)
	}
	var removed bool
	for _, r := range results {
		if r.RelPath == cursor.ReviewerRelPath && r.Action == cursor.ActionRemoved {
			removed = true
		}
	}
	if !removed {
		t.Fatalf("expected reviewer file to be removed once model is unset, got %+v", results)
	}
	if _, err := os.Stat(filepath.Join(root, cursor.ReviewerRelPath)); !os.IsNotExist(err) {
		t.Fatal("expected reviewer file to no longer exist")
	}
}

func TestInitSync_PreserveUnrelatedCursorFiles(t *testing.T) {
	root := t.TempDir()
	unrelated := filepath.Join(root, ".cursor", "rules", "my-rule.md")
	if err := os.MkdirAll(filepath.Dir(unrelated), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(unrelated, []byte("# my rule\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := cursor.Init(root, cfgWithCursor("gpt-5"), false); err != nil {
		t.Fatal(err)
	}
	if _, err := cursor.Sync(root, cfgWithCursor("gpt-5"), false); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(unrelated)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "# my rule\n" {
		t.Fatal("expected unrelated Cursor file to be preserved untouched")
	}
}

func TestInit_RefusesUnmanagedCollisionWithoutAdopt(t *testing.T) {
	root := t.TempDir()
	full := filepath.Join(root, cursor.SkillRelPath)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte("# hand-written skill\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := cursor.Init(root, config.Config{}, false)
	if err == nil {
		t.Fatal("expected an error for an unmanaged collision without --adopt")
	}
	var collisionErr *cursor.CollisionError
	if !errors.As(err, &collisionErr) {
		t.Fatalf("expected a CollisionError, got %v (%T)", err, err)
	}

	data, err := os.ReadFile(full)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "# hand-written skill\n" {
		t.Fatal("expected the unmanaged file to be left untouched on refusal")
	}
}

func TestInit_AdoptsUnmanagedFileWhenRequested(t *testing.T) {
	root := t.TempDir()
	full := filepath.Join(root, cursor.SkillRelPath)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte("# hand-written skill\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	results, err := cursor.Init(root, config.Config{}, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Action != cursor.ActionAdopted {
		t.Fatalf("expected an adopted action, got %+v", results)
	}
	assertFileContainsMarker(t, full)
}

func TestCheck_ReportsMissingProjection(t *testing.T) {
	root := t.TempDir()
	drift, err := cursor.Check(root, cfgWithCursor("gpt-5"))
	if err != nil {
		t.Fatal(err)
	}
	if len(drift) != 2 {
		t.Fatalf("expected both projections to be reported missing, got %+v", drift)
	}
	for _, d := range drift {
		if d.Kind != cursor.DriftMissing {
			t.Fatalf("expected missing drift, got %s for %s", d.Kind, d.RelPath)
		}
	}
}

func TestCheck_ReportsStaleAfterManualEdit(t *testing.T) {
	root := t.TempDir()
	cfg := cfgWithCursor("gpt-5")
	if _, err := cursor.Sync(root, cfg, false); err != nil {
		t.Fatal(err)
	}
	full := filepath.Join(root, cursor.ReviewerRelPath)
	original, err := os.ReadFile(full)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, append(original, []byte("\ntampered\n")...), 0o644); err != nil {
		t.Fatal(err)
	}

	drift, err := cursor.Check(root, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(drift) != 1 || drift[0].RelPath != cursor.ReviewerRelPath || drift[0].Kind != cursor.DriftStale {
		t.Fatalf("expected exactly one stale drift entry for the reviewer, got %+v", drift)
	}
}

func TestCheck_ReportsNoDriftWhenCurrent(t *testing.T) {
	root := t.TempDir()
	cfg := cfgWithCursor("gpt-5", "docs/guide.md")
	if _, err := cursor.Sync(root, cfg, false); err != nil {
		t.Fatal(err)
	}
	drift, err := cursor.Check(root, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(drift) != 0 {
		t.Fatalf("expected no drift immediately after sync, got %+v", drift)
	}
}

func TestCheck_ReportsShadowedUnmanagedFile(t *testing.T) {
	root := t.TempDir()
	full := filepath.Join(root, cursor.SkillRelPath)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte("# unmanaged\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	drift, err := cursor.Check(root, config.Config{})
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, d := range drift {
		if d.RelPath == cursor.SkillRelPath && d.Kind == cursor.DriftShadowed {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a shadowed drift entry for the unmanaged skill file, got %+v", drift)
	}
}

func TestCheck_ReportsStaleReviewerThatShouldBeRemoved(t *testing.T) {
	root := t.TempDir()
	if _, err := cursor.Sync(root, cfgWithCursor("gpt-5"), false); err != nil {
		t.Fatal(err)
	}
	drift, err := cursor.Check(root, config.Config{})
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, d := range drift {
		if d.RelPath == cursor.ReviewerRelPath && d.Kind == cursor.DriftShouldGo {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected the reviewer to be flagged for removal once the model is unset, got %+v", drift)
	}
}

func TestWriteManagedFile_RefusesSymlink(t *testing.T) {
	root := t.TempDir()
	full := filepath.Join(root, cursor.SkillRelPath)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	elsewhere := filepath.Join(root, "elsewhere.md")
	if err := os.WriteFile(elsewhere, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(elsewhere, full); err != nil {
		t.Skipf("symlinks unsupported in this environment: %v", err)
	}

	_, err := cursor.Init(root, config.Config{}, true)
	if err == nil {
		t.Fatal("expected an error writing through a symlink")
	}
}

func assertFileContainsMarker(t *testing.T, path string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), cursor.GeneratedMarker) {
		t.Fatalf("expected %s to contain the generated marker, got:\n%s", path, data)
	}
}
