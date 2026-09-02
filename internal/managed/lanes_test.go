package managed_test

import (
	"context"
	"testing"
)

func TestStagePlan_LaneFullCommandRunsWithoutCommandsTest(t *testing.T) {
	const cfg = `version: 1
validation:
  lanes:
    go:
      paths:
        - "*.go"
      full:
        - "true"
`
	opts := stagePlanOptions(t, "G-lane-full", "M-plan", cfg)
	res := runManaged(t, context.Background(), opts)
	if res.exitCode != 0 {
		t.Fatalf("expected exit 0, got %d (stderr: %s)", res.exitCode, res.stderr)
	}
	if got := stageOutcome(t, res.events, "test"); got != "passed" {
		t.Errorf("test outcome = %q, want passed", got)
	}
}

func TestStagePlan_LaneFullCommandFailureIsRetryable(t *testing.T) {
	const cfg = `version: 1
validation:
  lanes:
    go:
      paths:
        - "*.go"
      full:
        - "false"
`
	opts := stagePlanOptions(t, "G-lane-fail", "M-plan", cfg)
	res := runManaged(t, context.Background(), opts)
	if got := terminalOutcome(t, res.events); got != "failed_retryable" {
		t.Errorf("expected outcome failed_retryable, got %q", got)
	}
}

func TestStagePlan_UnmatchedLaneLeavesTestNotConfigured(t *testing.T) {
	const cfg = `version: 1
commands:
  lint: "true"
validation:
  lanes:
    docs:
      paths:
        - "docs/**"
      full:
        - "true"
`
	opts := stagePlanOptions(t, "G-lane-unmatched", "M-plan", cfg)
	res := runManaged(t, context.Background(), opts)
	if res.exitCode != 0 {
		t.Fatalf("expected exit 0, got %d (stderr: %s)", res.exitCode, res.stderr)
	}
	if got := stageOutcome(t, res.events, "test"); got != "not_configured" {
		t.Errorf("test outcome = %q, want not_configured", got)
	}
}
