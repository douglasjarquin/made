package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func mustMkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
}

func mustWrite(t *testing.T, path, contents string) {
	t.Helper()
	mustMkdir(t, filepath.Dir(path))
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestLocate_RootOnly(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, ".made.yaml"), "version: 1\n")

	loc, err := Locate(dir)
	if err != nil {
		t.Fatalf("Locate returned unexpected error: %v", err)
	}
	if loc.Layout != LayoutRoot {
		t.Fatalf("Layout = %q, want %q", loc.Layout, LayoutRoot)
	}
	if loc.Path != filepath.Join(dir, ".made.yaml") {
		t.Fatalf("Path = %q, want %q", loc.Path, filepath.Join(dir, ".made.yaml"))
	}
	if loc.Warning != "" {
		t.Fatalf("Warning = %q, want empty", loc.Warning)
	}
}

func TestLocate_DirectoryOnly(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, ".made", "config.yaml"), "version: 1\n")

	loc, err := Locate(dir)
	if err != nil {
		t.Fatalf("Locate returned unexpected error: %v", err)
	}
	if loc.Layout != LayoutDirectory {
		t.Fatalf("Layout = %q, want %q", loc.Layout, LayoutDirectory)
	}
	if loc.Path != filepath.Join(dir, ".made", "config.yaml") {
		t.Fatalf("Path = %q, want %q", loc.Path, filepath.Join(dir, ".made", "config.yaml"))
	}
}

func TestLocate_DirectoryLayoutIgnoresSiblingContent(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, ".made", "config.yaml"), "version: 1\n")
	mustWrite(t, filepath.Join(dir, ".made", "features", "README.md"), "# features\n")

	loc, err := Locate(dir)
	if err != nil {
		t.Fatalf("Locate returned unexpected error: %v", err)
	}
	if loc.Layout != LayoutDirectory {
		t.Fatalf("Layout = %q, want %q", loc.Layout, LayoutDirectory)
	}
}

func TestLocate_Neither(t *testing.T) {
	dir := t.TempDir()

	loc, err := Locate(dir)
	if err != nil {
		t.Fatalf("Locate returned unexpected error: %v", err)
	}
	if loc.Layout != LayoutAbsent {
		t.Fatalf("Layout = %q, want %q", loc.Layout, LayoutAbsent)
	}
	if loc.Path != "" {
		t.Fatalf("Path = %q, want empty", loc.Path)
	}
}

func TestLocate_BothFirstClassConflict(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, ".made.yaml"), "version: 1\n")
	mustWrite(t, filepath.Join(dir, ".made", "config.yaml"), "version: 1\n")

	loc, err := Locate(dir)
	if err == nil {
		t.Fatalf("Locate returned nil error, want conflict error")
	}
	if loc.Layout != LayoutConflict {
		t.Fatalf("Layout = %q, want %q", loc.Layout, LayoutConflict)
	}
	var conflict *ConflictError
	if !AsConflictError(err, &conflict) {
		t.Fatalf("error is not a *ConflictError: %v", err)
	}
	if len(conflict.Paths) != 2 {
		t.Fatalf("conflict.Paths = %v, want 2 entries", conflict.Paths)
	}
}

func TestLocate_LegacyOnlyWarns(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, ".made.yml"), "version: 1\n")

	loc, err := Locate(dir)
	if err != nil {
		t.Fatalf("Locate returned unexpected error: %v", err)
	}
	if loc.Layout != LayoutLegacy {
		t.Fatalf("Layout = %q, want %q", loc.Layout, LayoutLegacy)
	}
	if loc.Warning == "" {
		t.Fatalf("Warning = empty, want a bounded deprecation warning")
	}
}

func TestLocate_LegacyPlusRootConflict(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, ".made.yml"), "version: 1\n")
	mustWrite(t, filepath.Join(dir, ".made.yaml"), "version: 1\n")

	loc, err := Locate(dir)
	if err == nil {
		t.Fatalf("Locate returned nil error, want conflict error")
	}
	if loc.Layout != LayoutConflict {
		t.Fatalf("Layout = %q, want %q", loc.Layout, LayoutConflict)
	}
}

func TestLocate_LegacyPlusDirectoryConflict(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, ".made.yml"), "version: 1\n")
	mustWrite(t, filepath.Join(dir, ".made", "config.yaml"), "version: 1\n")

	loc, err := Locate(dir)
	if err == nil {
		t.Fatalf("Locate returned nil error, want conflict error")
	}
	if loc.Layout != LayoutConflict {
		t.Fatalf("Layout = %q, want %q", loc.Layout, LayoutConflict)
	}
}

func TestLocate_RootIsSymlinkRejected(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks not exercised on windows")
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "real.yaml")
	mustWrite(t, target, "version: 1\n")
	link := filepath.Join(dir, ".made.yaml")
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	_, err := Locate(dir)
	if err == nil {
		t.Fatalf("Locate returned nil error, want rejection of symlinked config")
	}
}

func TestLocate_RootIsDirectoryRejected(t *testing.T) {
	dir := t.TempDir()
	mustMkdir(t, filepath.Join(dir, ".made.yaml"))

	_, err := Locate(dir)
	if err == nil {
		t.Fatalf("Locate returned nil error, want rejection of directory at config path")
	}
}

func TestLocate_MadeDirIsSymlinkRejected(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks not exercised on windows")
	}
	dir := t.TempDir()
	realDir := t.TempDir()
	mustWrite(t, filepath.Join(realDir, "config.yaml"), "version: 1\n")
	if err := os.Symlink(realDir, filepath.Join(dir, ".made")); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	_, err := Locate(dir)
	if err == nil {
		t.Fatalf("Locate returned nil error, want rejection of symlinked .made directory")
	}
}

func TestLocate_FIFORejectedWithoutBlocking(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("FIFOs not supported on windows")
	}
	dir := t.TempDir()
	fifoPath := filepath.Join(dir, ".made.yaml")
	if err := mkfifo(fifoPath); err != nil {
		t.Skipf("mkfifo unsupported in this environment: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		_, err := Locate(dir)
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatalf("Locate returned nil error, want rejection of FIFO")
		}
	case <-timeoutChan():
		t.Fatalf("Locate blocked on FIFO instead of rejecting it")
	}
}

func TestLocate_NestedInvocationUsesGivenRoot(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, ".made.yaml"), "version: 1\n")
	nested := filepath.Join(dir, "cmd", "sub")
	mustMkdir(t, nested)

	loc, err := Locate(nested)
	if err != nil {
		t.Fatalf("Locate returned unexpected error: %v", err)
	}
	if loc.Layout != LayoutAbsent {
		t.Fatalf("Layout = %q, want %q (locate does not search ancestors)", loc.Layout, LayoutAbsent)
	}
}
