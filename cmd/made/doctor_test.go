package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestDoctor_DaemonDownReportsAccurately(t *testing.T) {
	home := shortTempDir(t)
	t.Setenv("MADE_HOME", home)

	out, errOut, code := runCapture(t, []string{"doctor"})
	combined := string(out) + string(errOut)

	if code != 1 {
		t.Fatalf("exit code = %d, want 1 (daemon down should fail doctor); stdout=%s stderr=%s", code, out, errOut)
	}

	lower := strings.ToLower(combined)
	if !strings.Contains(lower, "daemon") || !strings.Contains(lower, "unreachable") {
		t.Fatalf("expected daemon-unreachable report, got stdout=%s stderr=%s", out, errOut)
	}
	if !strings.Contains(lower, "gh:") {
		t.Fatalf("expected gh check to still run independently of the daemon check, got stdout=%s stderr=%s", out, errOut)
	}
	if !strings.Contains(lower, "herdr:") {
		t.Fatalf("expected herdr check to still run and report informationally, got stdout=%s stderr=%s", out, errOut)
	}
}

func TestDoctor_GateNotInitializedIsInformationalOnly(t *testing.T) {
	home := shortTempDir(t)
	t.Setenv("MADE_HOME", home)

	scratch := shortTempDir(t)

	out, errOut, code := runCapture(t, []string{"doctor", scratch})
	combined := strings.ToLower(string(out) + string(errOut))

	if !strings.Contains(combined, "gate: not initialized") {
		t.Fatalf("expected gate: not initialized report, got stdout=%s stderr=%s", out, errOut)
	}
	if code != 1 {
		t.Fatalf("exit code = %d, want 1 (daemon/gh unreachable, unaffected by gate check); stdout=%s stderr=%s", code, out, errOut)
	}
}

func TestDoctor_GateInitializedIsReported(t *testing.T) {
	home := shortTempDir(t)
	t.Setenv("MADE_HOME", home)

	scratch := shortTempDir(t)
	remoteDir := filepath.Join(scratch, "remote.git")
	sourceDir := filepath.Join(scratch, "source")
	remoteURL := "file://" + remoteDir

	testGit(t, "", "init", "--bare", "-b", "main", remoteDir)
	testGit(t, "", "init", "-b", "main", sourceDir)
	writeAndCommit(t, sourceDir, "README.md", "hello\n", "init")
	testGit(t, sourceDir, "remote", "add", "origin", remoteURL)
	testGit(t, sourceDir, "push", "origin", "main")

	if _, errOut, code := runCapture(t, []string{"gate", "init", sourceDir, remoteURL}); code != 0 {
		t.Fatalf("gate init exit code = %d, want 0; stderr=%s", code, errOut)
	}

	out, errOut, _ := runCapture(t, []string{"doctor", sourceDir})
	combined := strings.ToLower(string(out) + string(errOut))
	if !strings.Contains(combined, "gate: initialized") {
		t.Fatalf("expected gate: initialized report, got stdout=%s stderr=%s", out, errOut)
	}
}
