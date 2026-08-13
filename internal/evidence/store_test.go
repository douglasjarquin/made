package evidence_test

import (
	"testing"

	"github.com/douglasjarquin/made/internal/evidence"
)

func TestNewStoreSelectsOrphanBranchByDefault(t *testing.T) {
	store := evidence.NewStore("/some/repo", evidence.Config{})
	if _, ok := store.(*evidence.OrphanBranchStore); !ok {
		t.Fatalf("expected default config to select *OrphanBranchStore, got %T", store)
	}
}

func TestNewStoreSelectsInRepoWhenConfigured(t *testing.T) {
	store := evidence.NewStore("/some/repo", evidence.Config{StoreInRepo: true, Dir: ".made/evidence"})
	inRepo, ok := store.(*evidence.InRepoStore)
	if !ok {
		t.Fatalf("expected StoreInRepo config to select *InRepoStore, got %T", store)
	}
	if inRepo.Dir != ".made/evidence" {
		t.Fatalf("expected configured dir to be threaded through, got %q", inRepo.Dir)
	}
}
