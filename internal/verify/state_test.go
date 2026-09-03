package verify_test

import (
	"testing"

	"github.com/douglasjarquin/made/internal/verify"
)

func TestStateRoot_DeterministicAndDistinct(t *testing.T) {
	a1 := verify.StateRoot("/repo/a")
	a2 := verify.StateRoot("/repo/a")
	b := verify.StateRoot("/repo/b")

	if a1 != a2 {
		t.Errorf("StateRoot is not deterministic: %q != %q", a1, a2)
	}
	if a1 == b {
		t.Errorf("StateRoot collided for distinct roots: %q", a1)
	}
}

func TestStateRoot_SubdirsAreDistinct(t *testing.T) {
	root := verify.StateRoot("/repo/a")
	req := verify.RequestsDir(root)
	ev := verify.EvidenceRoot(root)
	rc := verify.ReceiptsDir(root)
	if req == ev || req == rc || ev == rc {
		t.Errorf("expected distinct subdirectories, got requests=%q evidence=%q receipts=%q", req, ev, rc)
	}
}
