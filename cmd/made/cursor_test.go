package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/douglasjarquin/made/internal/api"
)

func newCursorTestRepo(t *testing.T, configContent string) string {
	t.Helper()
	dir := shortTempDir(t)
	gitVerifyAt(t, dir, "init", "-b", "main")
	gitVerifyAt(t, dir, "config", "user.email", "test@test.local")
	gitVerifyAt(t, dir, "config", "user.name", "test")
	if configContent != "" {
		if err := os.WriteFile(filepath.Join(dir, ".made.yaml"), []byte(configContent), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitVerifyAt(t, dir, "add", ".")
	gitVerifyAt(t, dir, "commit", "-m", "initial")
	return dir
}

const cursorConfigYAML = "version: 1\nreview:\n  executors:\n    cursor:\n      model: \"claude-opus-5[effort=high,context=300k]\"\n"

func TestCursorInitThenCheck_CLIRoundTrip(t *testing.T) {
	dir := newCursorTestRepo(t, cursorConfigYAML)

	_, stderr, code := runCapture(t, []string{"cursor", "init", "--repo", dir, "--json"})
	if code != 0 {
		t.Fatalf("cursor init: exit %d stderr=%s", code, stderr)
	}
	if _, err := os.Stat(filepath.Join(dir, ".cursor", "agents", "made-reviewer.md")); err != nil {
		t.Fatalf("expected made-reviewer.md to be created: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".cursor", "skills", "verify-with-made", "SKILL.md")); err != nil {
		t.Fatalf("expected SKILL.md to be created: %v", err)
	}

	_, stderr, code = runCapture(t, []string{"cursor", "check", "--repo", dir, "--json"})
	if code != 0 {
		t.Fatalf("cursor check: exit %d (expected no drift right after init) stderr=%s", code, stderr)
	}
}

func TestCursorCheck_FailsBeforeInit(t *testing.T) {
	dir := newCursorTestRepo(t, cursorConfigYAML)
	_, _, code := runCapture(t, []string{"cursor", "check", "--repo", dir, "--json"})
	if code == 0 {
		t.Fatal("expected cursor check to fail before init/sync has run")
	}
}

func TestCursorSync_ReflectsModelChange(t *testing.T) {
	dir := newCursorTestRepo(t, cursorConfigYAML)
	if _, stderr, code := runCapture(t, []string{"cursor", "sync", "--repo", dir}); code != 0 {
		t.Fatalf("cursor sync: exit %d stderr=%s", code, stderr)
	}

	if err := os.WriteFile(filepath.Join(dir, ".made.yaml"), []byte("version: 1\nreview:\n  executors:\n    cursor:\n      model: \"gpt-6\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, stderr, code := runCapture(t, []string{"cursor", "sync", "--repo", dir}); code != 0 {
		t.Fatalf("cursor sync (2nd): exit %d stderr=%s", code, stderr)
	}

	data, err := os.ReadFile(filepath.Join(dir, ".cursor", "agents", "made-reviewer.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !containsAll(string(data), "model: gpt-6") {
		t.Fatalf("expected the reviewer to reflect the updated model, got:\n%s", data)
	}
}

func TestCursorInit_RefusesUnmanagedCollisionCLI(t *testing.T) {
	dir := newCursorTestRepo(t, "version: 1\n")
	skillPath := filepath.Join(dir, ".cursor", "skills", "verify-with-made", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(skillPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(skillPath, []byte("# hand-written\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, stderr, code := runCapture(t, []string{"cursor", "init", "--repo", dir})
	if code == 0 {
		t.Fatal("expected cursor init to refuse an unmanaged collision without --adopt")
	}
	if len(stderr) == 0 {
		t.Error("expected a diagnostic on stderr")
	}

	_, stderr, code = runCapture(t, []string{"cursor", "init", "--repo", dir, "--adopt"})
	if code != 0 {
		t.Fatalf("expected cursor init --adopt to succeed, exit %d stderr=%s", code, stderr)
	}
}

func TestCursorDoctor_HealthyAfterSyncOnConfiguredRepo(t *testing.T) {
	dir := newCursorTestRepo(t, cursorConfigYAML)
	if _, stderr, code := runCapture(t, []string{"cursor", "sync", "--repo", dir}); code != 0 {
		t.Fatalf("cursor sync: exit %d stderr=%s", code, stderr)
	}

	stdout, stderr, code := runCapture(t, []string{"cursor", "doctor", "--repo", dir, "--base-ref", "HEAD", "--json"})
	if code != 0 {
		t.Fatalf("cursor doctor: exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	var report cursorDoctorReport
	if err := json.Unmarshal(stdout, &report); err != nil {
		t.Fatalf("parse doctor JSON: %v (stdout=%s)", err, stdout)
	}
	if !report.Healthy {
		t.Fatalf("expected a healthy doctor report, got %+v", report)
	}
}

func TestCursorDoctor_UnhealthyWithoutConfig(t *testing.T) {
	dir := newCursorTestRepo(t, "")
	_, _, code := runCapture(t, []string{"cursor", "doctor", "--repo", dir, "--json"})
	if code == 0 {
		t.Fatal("expected cursor doctor to fail with no configuration")
	}
}

func TestCursorCommands_NeverTouchDaemonGateOrGitHub(t *testing.T) {
	dir := newCursorTestRepo(t, cursorConfigYAML)

	madeHomeDir := shortTempDir(t)
	t.Setenv("MADE_HOME", madeHomeDir)

	fakeBinDir := shortTempDir(t)
	sentinel := filepath.Join(fakeBinDir, "gh-invoked")
	writeFakeGh(t, fakeBinDir, sentinel)
	t.Setenv("PATH", fakeBinDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	for _, args := range [][]string{
		{"cursor", "init", "--repo", dir, "--json"},
		{"cursor", "sync", "--repo", dir, "--json"},
		{"cursor", "check", "--repo", dir, "--json"},
		{"cursor", "doctor", "--repo", dir, "--json"},
	} {
		if _, stderr, code := runCapture(t, args); code != 0 {
			t.Fatalf("%v: exit %d stderr=%s", args, code, stderr)
		}
	}

	if _, err := os.Stat(filepath.Join(madeHomeDir, "gates")); !os.IsNotExist(err) {
		t.Fatalf("expected no gate directory to be created under MADE_HOME, err=%v", err)
	}
	if _, err := os.Stat(api.SocketPath(madeHomeDir)); !os.IsNotExist(err) {
		t.Fatalf("expected no daemon socket to be created under MADE_HOME, err=%v", err)
	}
	if _, err := os.Stat(sentinel); !os.IsNotExist(err) {
		t.Fatal("expected gh to never be invoked by any made cursor command")
	}
}

// TestSkillNoReviewPath_MadeVerifyRunSkipsReviewTruthfully proves the exact
// behavior the generated skill documents for its step 7 ("Review is not
// configured for Cursor"): with no internal agent and review.required
// false, `made verify run` truthfully skips the Review stage entirely (no
// subagent, no external interaction) and still runs the remaining
// configured stages to a real pass - it is not a documentation-only claim.
func TestSkillNoReviewPath_MadeVerifyRunSkipsReviewTruthfully(t *testing.T) {
	dir := newVerifyTestRepo(t) // configures commands.test/lint only, no review, no agent, no cursor executor

	stdout, stderr, code := runCapture(t, []string{"verify", "run", "--repo", dir, "--base-ref", "origin/main", "--json"})
	if code != 0 {
		t.Fatalf("verify run: exit %d stderr=%s", code, stderr)
	}
	var receipt struct {
		Outcome string `json:"outcome"`
		Review  *struct {
			Source string `json:"source"`
		} `json:"review,omitempty"`
		Stages []struct {
			Name   string `json:"name"`
			Status string `json:"status"`
		} `json:"stages"`
	}
	if err := json.Unmarshal(stdout, &receipt); err != nil {
		t.Fatalf("parse receipt JSON: %v (stdout=%s)", err, stdout)
	}
	if receipt.Outcome != "passed" {
		t.Fatalf("outcome = %q, want passed", receipt.Outcome)
	}
	for _, s := range receipt.Stages {
		if s.Name == "review" && s.Status != "not_configured" {
			t.Fatalf("expected review stage status not_configured, got %q", s.Status)
		}
	}
}

func writeFakeGh(t *testing.T, dir, sentinel string) {
	t.Helper()
	path := filepath.Join(dir, "gh")
	script := "#!/bin/sh\ntouch \"" + sentinel + "\"\nexit 1\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS == "windows" {
		t.Skip("fake gh shell script requires a POSIX shell")
	}
}

func containsAll(haystack string, needles ...string) bool {
	for _, n := range needles {
		if !strings.Contains(haystack, n) {
			return false
		}
	}
	return true
}
