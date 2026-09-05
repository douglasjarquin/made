package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeStubBinary(t *testing.T, dir, name string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write stub binary %s: %v", name, err)
	}
}

func TestDoctor_AgentResolutionReportsResolvedKind(t *testing.T) {
	home := shortTempDir(t)
	t.Setenv("MADE_HOME", home)

	scratch := shortTempDir(t)
	if err := os.WriteFile(filepath.Join(scratch, ".made.yaml"), []byte("version: 1\nagent: auto\nagents: [claude]\n"), 0o644); err != nil {
		t.Fatalf("write .made.yaml: %v", err)
	}
	pathDir := shortTempDir(t)
	writeStubBinary(t, pathDir, "claude")
	t.Setenv("PATH", pathDir)

	out, errOut, _ := runCapture(t, []string{"doctor", scratch})
	combined := strings.ToLower(string(out) + string(errOut))
	if !strings.Contains(combined, "agent:") || !strings.Contains(combined, "claude") || !strings.Contains(combined, "resolved") {
		t.Fatalf("expected agent: claude (resolved) report, got stdout=%s stderr=%s", out, errOut)
	}
}

func TestDoctor_AgentResolutionJSONReportsAllExhausted(t *testing.T) {
	home := shortTempDir(t)
	t.Setenv("MADE_HOME", home)

	scratch := shortTempDir(t)
	if err := os.WriteFile(filepath.Join(scratch, ".made.yaml"), []byte("version: 1\nagent: auto\nagents: [claude]\n"), 0o644); err != nil {
		t.Fatalf("write .made.yaml: %v", err)
	}
	t.Setenv("PATH", shortTempDir(t)) // empty PATH: claude is missing.

	out, _, _ := runCapture(t, []string{"doctor", "--json", scratch})
	var report struct {
		Checks map[string]string `json:"checks"`
	}
	if err := json.Unmarshal(out, &report); err != nil {
		t.Fatalf("parse doctor --json output: %v; stdout=%s", err, out)
	}
	summary := report.Checks["agent_resolution"]
	if !strings.Contains(summary, "claude") || !strings.Contains(summary, "missing") {
		t.Fatalf("checks.agent_resolution = %q, want it to name claude as missing", summary)
	}
}

func TestDoctor_AgentResolutionReportsPinnedAgentWithoutProbing(t *testing.T) {
	home := shortTempDir(t)
	t.Setenv("MADE_HOME", home)

	scratch := shortTempDir(t)
	if err := os.WriteFile(filepath.Join(scratch, ".made.yaml"), []byte("version: 1\nagent: claude\n"), 0o644); err != nil {
		t.Fatalf("write .made.yaml: %v", err)
	}
	t.Setenv("PATH", shortTempDir(t)) // empty PATH: pinned path must not probe at all.

	out, errOut, _ := runCapture(t, []string{"doctor", scratch})
	combined := strings.ToLower(string(out) + string(errOut))
	if !strings.Contains(combined, "agent:") || !strings.Contains(combined, "claude") || !strings.Contains(combined, "pinned") {
		t.Fatalf("expected agent: claude (pinned) report, got stdout=%s stderr=%s", out, errOut)
	}
}
