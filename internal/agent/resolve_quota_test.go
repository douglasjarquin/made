package agent

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// stubPresentAndAuthenticated puts a trivial "exit 0" script on a scoped
// PATH for each kind, so Resolve's presence and auth steps both pass -
// this test only exercises the quota step (via stubQuotaProbe), which
// resolve_test.go's black-box tests can't reach directly since quotaProbe
// is unexported.
func stubPresentAndAuthenticated(t *testing.T, kinds ...Kind) {
	t.Helper()
	dir := t.TempDir()
	for _, kind := range kinds {
		path := filepath.Join(dir, kind.BinaryName())
		if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
			t.Fatalf("write stub binary %s: %v", path, err)
		}
	}
	t.Setenv("PATH", dir)
}

func stubQuotaProbe(t *testing.T, exhausted map[Kind]*QuotaSignal) {
	t.Helper()
	original := quotaProbe
	quotaProbe = func(ctx context.Context, kind Kind) (*QuotaSignal, error) {
		return exhausted[kind], nil
	}
	t.Cleanup(func() { quotaProbe = original })
}

func TestResolve_QuotaExhaustedSkipsToNext(t *testing.T) {
	stubPresentAndAuthenticated(t, KindClaude, KindCodex)
	resetsAt := time.Date(2026, 9, 10, 6, 0, 0, 0, time.UTC)
	stubQuotaProbe(t, map[Kind]*QuotaSignal{
		KindClaude: {Exhausted: true, ResetsAt: &resetsAt},
	})

	res := Resolve(context.Background(), []Kind{KindClaude, KindCodex})

	if res.Selected == nil || *res.Selected != KindCodex {
		t.Fatalf("Resolve().Selected = %v, want codex", res.Selected)
	}
	if len(res.Attempts) != 1 || res.Attempts[0].Kind != KindClaude || res.Attempts[0].Reason != ReasonQuotaExhausted {
		t.Errorf("Resolve().Attempts = %+v, want one quota_exhausted attempt for claude", res.Attempts)
	}
	if res.Attempts[0].QuotaResetsAt == nil || !res.Attempts[0].QuotaResetsAt.Equal(resetsAt) {
		t.Errorf("Resolve().Attempts[0].QuotaResetsAt = %v, want %v", res.Attempts[0].QuotaResetsAt, resetsAt)
	}
}
