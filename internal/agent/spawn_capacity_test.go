package agent

import (
	"errors"
	"fmt"
	"testing"
)

func TestIsCapacityStderr_ClaudeUsageLanguageClassified(t *testing.T) {
	if !isCapacityStderr(KindClaude, "Claude usage limit reached, try again later") {
		t.Errorf("isCapacityStderr(claude, usage-limit stderr) = false, want true")
	}
}

func TestIsCapacityStderr_CodexUsageLanguageClassified(t *testing.T) {
	if !isCapacityStderr(KindCodex, "codex: usage limit reached, please retry") {
		t.Errorf("isCapacityStderr(codex, usage-limit stderr) = false, want true")
	}
}

func TestIsCapacityStderr_UnrelatedStderrNotClassified(t *testing.T) {
	if isCapacityStderr(KindClaude, "invalid schema: missing required property") {
		t.Errorf("isCapacityStderr(claude, unrelated stderr) = true, want false")
	}
}

func TestIsCapacityStderr_CursorGrokNeverClassified(t *testing.T) {
	for _, kind := range []Kind{KindCursor, KindGrok} {
		if isCapacityStderr(kind, "usage limit reached, rate limit exceeded, quota depleted") {
			t.Errorf("isCapacityStderr(%s, quota-language stderr) = true, want false (D2: presence-only, no classification)", kind)
		}
	}
}

func TestSpawn_ExitErrorWrapsErrAgentCapacityWhenClassified(t *testing.T) {
	exitErr := fmt.Errorf("agent: %s (%s) exited %d: stdout=%s stderr=%s", KindClaude, "claude", 1, "", "usage limit reached")
	wrapped := fmt.Errorf("%w: %w", ErrAgentCapacity, exitErr)
	if !errors.Is(wrapped, ErrAgentCapacity) {
		t.Errorf("errors.Is(wrapped, ErrAgentCapacity) = false, want true")
	}
	if !errors.Is(wrapped, exitErr) {
		t.Errorf("errors.Is(wrapped, exitErr) = false, want true (original error text preserved)")
	}
}
