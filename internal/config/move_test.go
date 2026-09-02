package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMove_RootToDirectory(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, RootConfigFileName), "version: 1\nno_ci: true\n")

	from, to, err := Move(dir, LayoutDirectory)
	if err != nil {
		t.Fatalf("Move: %v", err)
	}
	if from != filepath.Join(dir, RootConfigFileName) {
		t.Fatalf("from = %q, want %q", from, filepath.Join(dir, RootConfigFileName))
	}
	wantTo := filepath.Join(dir, DirectoryName, DirectoryConfigFileName)
	if to != wantTo {
		t.Fatalf("to = %q, want %q", to, wantTo)
	}
	if _, err := os.Stat(from); !os.IsNotExist(err) {
		t.Fatalf("source %s still exists after move", from)
	}
	got, err := os.ReadFile(to)
	if err != nil {
		t.Fatalf("read moved file: %v", err)
	}
	if string(got) != "version: 1\nno_ci: true\n" {
		t.Fatalf("moved content = %q, want exact bytes preserved", got)
	}
}

func TestMove_DirectoryToRoot(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, DirectoryName, DirectoryConfigFileName), "version: 1\nno_ci: true\n")

	from, to, err := Move(dir, LayoutRoot)
	if err != nil {
		t.Fatalf("Move: %v", err)
	}
	if to != filepath.Join(dir, RootConfigFileName) {
		t.Fatalf("to = %q, want %q", to, filepath.Join(dir, RootConfigFileName))
	}
	if _, err := os.Stat(from); !os.IsNotExist(err) {
		t.Fatalf("source %s still exists after move", from)
	}
}

func TestMove_LegacyToRoot(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, LegacyConfigFileName), "version: 1\nno_ci: true\n")

	from, to, err := Move(dir, LayoutRoot)
	if err != nil {
		t.Fatalf("Move: %v", err)
	}
	if from != filepath.Join(dir, LegacyConfigFileName) {
		t.Fatalf("from = %q, want legacy path", from)
	}
	if to != filepath.Join(dir, RootConfigFileName) {
		t.Fatalf("to = %q, want root path", to)
	}
}

func TestMove_LegacyToDirectory(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, LegacyConfigFileName), "version: 1\nno_ci: true\n")

	_, to, err := Move(dir, LayoutDirectory)
	if err != nil {
		t.Fatalf("Move: %v", err)
	}
	if to != filepath.Join(dir, DirectoryName, DirectoryConfigFileName) {
		t.Fatalf("to = %q, want directory path", to)
	}
}

func TestMove_PreservesUnrelatedDirectoryContent(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, RootConfigFileName), "version: 1\nno_ci: true\n")
	mustWrite(t, filepath.Join(dir, DirectoryName, "features", "README.md"), "# features\n")

	_, _, err := Move(dir, LayoutDirectory)
	if err != nil {
		t.Fatalf("Move: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dir, DirectoryName, "features", "README.md"))
	if err != nil {
		t.Fatalf("unrelated .made/ content lost after move: %v", err)
	}
	if string(got) != "# features\n" {
		t.Fatalf("unrelated content mutated: %q", got)
	}
}

func TestMove_RefusesWhenDestinationExists(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, LegacyConfigFileName), "version: 1\n")
	mustWrite(t, filepath.Join(dir, RootConfigFileName), "version: 1\nno_ci: true\n")

	_, _, err := Move(dir, LayoutRoot)
	if err == nil {
		t.Fatalf("Move returned nil error, want refusal (both legacy and root present is a conflict, not a valid move source)")
	}
}

func TestMove_RefusesWhenAbsent(t *testing.T) {
	dir := t.TempDir()

	_, _, err := Move(dir, LayoutRoot)
	if err == nil {
		t.Fatalf("Move returned nil error, want refusal (no configuration to move)")
	}
}

func TestMove_RefusesWhenAlreadyAtTargetLayout(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, RootConfigFileName), "version: 1\nno_ci: true\n")

	_, _, err := Move(dir, LayoutRoot)
	if err == nil {
		t.Fatalf("Move returned nil error, want refusal (already at root layout)")
	}
}

func TestMove_NeverOverwritesUnrelatedDestination(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, LegacyConfigFileName), "version: 1\nno_ci: true\n")
	mustWrite(t, filepath.Join(dir, DirectoryName, DirectoryConfigFileName), "version: 1\npre-existing: unrelated\n")

	_, _, err := Move(dir, LayoutDirectory)
	if err == nil {
		t.Fatalf("Move returned nil error, want refusal (destination already exists)")
	}
	got, readErr := os.ReadFile(filepath.Join(dir, DirectoryName, DirectoryConfigFileName))
	if readErr != nil {
		t.Fatalf("read destination: %v", readErr)
	}
	if string(got) != "version: 1\npre-existing: unrelated\n" {
		t.Fatalf("destination mutated by refused move: %q", got)
	}
}
