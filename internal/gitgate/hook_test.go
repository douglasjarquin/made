package gitgate_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/douglasjarquin/made/internal/gitgate"
)

func TestInstallHooksBakesBinaryPathAndMadeHomeIntoPreReceive(t *testing.T) {
	dir := t.TempDir()
	barePath := filepath.Join(dir, "gate.git")
	if err := gitgate.InitBare(barePath); err != nil {
		t.Fatalf("InitBare: %v", err)
	}

	madeBinaryPath := filepath.Join(dir, "bin", "made")
	madeHome := filepath.Join(dir, "made-home")

	if err := gitgate.InstallHooks(barePath, madeBinaryPath, madeHome); err != nil {
		t.Fatalf("InstallHooks: %v", err)
	}

	preReceive, err := os.ReadFile(filepath.Join(barePath, "hooks", "pre-receive"))
	if err != nil {
		t.Fatalf("read pre-receive hook: %v", err)
	}
	script := string(preReceive)

	if !strings.Contains(script, madeBinaryPath) {
		t.Fatalf("expected pre-receive script to contain made binary path %q, got:\n%s", madeBinaryPath, script)
	}
	if !strings.Contains(script, madeHome) {
		t.Fatalf("expected pre-receive script to contain MADE_HOME %q, got:\n%s", madeHome, script)
	}
	if !strings.Contains(script, "gate admit-push") {
		t.Fatalf("expected pre-receive script to invoke gate admit-push, got:\n%s", script)
	}
	if !strings.Contains(script, barePath) {
		t.Fatalf("expected pre-receive script to reference the gate repo path %q, got:\n%s", barePath, script)
	}
}

func TestInstallHooksPostReceiveAlwaysExitsZero(t *testing.T) {
	dir := t.TempDir()
	barePath := filepath.Join(dir, "gate.git")
	if err := gitgate.InitBare(barePath); err != nil {
		t.Fatalf("InitBare: %v", err)
	}

	madeBinaryPath := filepath.Join(dir, "bin", "made")
	madeHome := filepath.Join(dir, "made-home")

	if err := gitgate.InstallHooks(barePath, madeBinaryPath, madeHome); err != nil {
		t.Fatalf("InstallHooks: %v", err)
	}

	postReceive, err := os.ReadFile(filepath.Join(barePath, "hooks", "post-receive"))
	if err != nil {
		t.Fatalf("read post-receive hook: %v", err)
	}
	script := strings.TrimRight(string(postReceive), "\n")

	if !strings.Contains(script, madeBinaryPath) {
		t.Fatalf("expected post-receive script to contain made binary path %q, got:\n%s", madeBinaryPath, script)
	}
	if !strings.Contains(script, madeHome) {
		t.Fatalf("expected post-receive script to contain MADE_HOME %q, got:\n%s", madeHome, script)
	}
	if !strings.Contains(script, "gate notify-push") {
		t.Fatalf("expected post-receive script to invoke gate notify-push, got:\n%s", script)
	}

	lines := strings.Split(script, "\n")
	last := strings.TrimSpace(lines[len(lines)-1])
	if last != "exit 0" {
		t.Fatalf("expected post-receive script to end with an unconditional exit 0, last line was %q, full script:\n%s", last, script)
	}
}
