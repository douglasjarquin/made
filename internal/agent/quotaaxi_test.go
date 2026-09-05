package agent

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func readQuotaFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "quotaaxi", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return data
}

func TestParseQuotaReport_ClaudeAuthenticatedNotExhausted(t *testing.T) {
	signal, err := parseQuotaReport(readQuotaFixture(t, "claude_authenticated.json"))
	if err != nil {
		t.Fatalf("parseQuotaReport() error = %v", err)
	}
	if signal != nil {
		t.Errorf("parseQuotaReport() = %+v, want nil (28%% remaining, not exhausted)", signal)
	}
}

func TestParseQuotaReport_CodexUnauthenticatedYieldsNoSignal(t *testing.T) {
	signal, err := parseQuotaReport(readQuotaFixture(t, "codex_unauthenticated.json"))
	if err != nil {
		t.Fatalf("parseQuotaReport() error = %v", err)
	}
	if signal != nil {
		t.Errorf("parseQuotaReport() = %+v, want nil (quotaSemantics.status=unknown)", signal)
	}
}

func TestParseQuotaReport_ClaudeExhaustedBelowThreshold(t *testing.T) {
	signal, err := parseQuotaReport(readQuotaFixture(t, "claude_exhausted.json"))
	if err != nil {
		t.Fatalf("parseQuotaReport() error = %v", err)
	}
	if signal == nil || !signal.Exhausted {
		t.Fatalf("parseQuotaReport() = %+v, want Exhausted=true (0.5%% remaining)", signal)
	}
}

func TestParseQuotaReport_LegacySchemaFallsBackToRawWindows(t *testing.T) {
	signal, err := parseQuotaReport(readQuotaFixture(t, "claude_legacy_schema_raw_windows_exhausted.json"))
	if err != nil {
		t.Fatalf("parseQuotaReport() error = %v", err)
	}
	if signal == nil || !signal.Exhausted {
		t.Fatalf("parseQuotaReport() = %+v, want Exhausted=true (raw five_hour window at 0%% remaining, no quotaSemantics)", signal)
	}
}

func TestProbeQuota_AbsentBinaryYieldsNoSignalNoError(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	signal, err := probeQuota(context.Background(), KindClaude)
	if err != nil {
		t.Fatalf("probeQuota() error = %v, want nil", err)
	}
	if signal != nil {
		t.Errorf("probeQuota() = %+v, want nil (quota-axi absent from PATH)", signal)
	}
}
