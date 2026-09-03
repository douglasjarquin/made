package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestLoadEffectiveConfig_StrictValidationAppliesToRootLayout(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, RootConfigFileName)
	if err := os.WriteFile(path, []byte("agent: codex\n"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	_, err := LoadEffectiveConfig(path, "")
	if err == nil {
		t.Fatalf("LoadEffectiveConfig returned nil error, want version:1 rejection for %s", RootConfigFileName)
	}
}

func TestLoadEffectiveConfig_StrictValidationAppliesToDirectoryLayout(t *testing.T) {
	dir := t.TempDir()
	madeDir := filepath.Join(dir, DirectoryName)
	if err := os.MkdirAll(madeDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	path := filepath.Join(madeDir, DirectoryConfigFileName)
	if err := os.WriteFile(path, []byte("agent: codex\n"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	_, err := LoadEffectiveConfig(path, "")
	if err == nil {
		t.Fatalf("LoadEffectiveConfig returned nil error, want version:1 rejection for %s", path)
	}
}

func TestLoadEffectiveConfig_DirectoryLayoutAcceptsVersionedConfig(t *testing.T) {
	dir := t.TempDir()
	madeDir := filepath.Join(dir, DirectoryName)
	if err := os.MkdirAll(madeDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	path := filepath.Join(madeDir, DirectoryConfigFileName)
	if err := os.WriteFile(path, []byte("version: 1\nreview:\n  required: true\nagent: codex\n"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	cfg, err := LoadEffectiveConfig(path, "")
	if err != nil {
		t.Fatalf("LoadEffectiveConfig: %v", err)
	}
	if !cfg.Review.Required {
		t.Fatalf("Review.Required = false, want true")
	}
}

func TestLoadEffectiveConfig_RejectsSymlinkedConfigFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks not exercised on windows")
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "real.yaml")
	if err := os.WriteFile(target, []byte("version: 1\nreview:\n  required: true\nagent: codex\n"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	link := filepath.Join(dir, RootConfigFileName)
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	_, err := LoadEffectiveConfig(link, "")
	if err == nil {
		t.Fatalf("LoadEffectiveConfig returned nil error, want rejection of symlinked config path")
	}
}

func TestLoadEffectiveConfig_RejectsFIFOWithoutBlocking(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("FIFOs not supported on windows")
	}
	dir := t.TempDir()
	fifoPath := filepath.Join(dir, RootConfigFileName)
	if err := mkfifo(fifoPath); err != nil {
		t.Skipf("mkfifo unsupported in this environment: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		_, err := LoadEffectiveConfig(fifoPath, "")
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatalf("LoadEffectiveConfig returned nil error, want rejection of FIFO")
		}
	case <-timeoutChan():
		t.Fatalf("LoadEffectiveConfig blocked on FIFO instead of rejecting it")
	}
}
