package intent_test

import (
	"strings"
	"testing"

	"github.com/douglasjarquin/made/internal/pipeline/intent"
)

func TestCheck_MissingIntentBlocks(t *testing.T) {
	repoPath := initRepoWithCommit(t, "Add feature\n\nNo trailer here.\n")

	result, err := intent.Check(repoPath)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if result.OK {
		t.Fatalf("expected OK=false for commit with no Intent trailer, got %+v", result)
	}
	if !strings.Contains(strings.ToLower(result.Message), "intent") {
		t.Fatalf("expected message to name the missing intent, got %q", result.Message)
	}
}

func TestCheck_EmptyIntentBlocks(t *testing.T) {
	repoPath := initRepoWithCommit(t, "Add feature\n\nIntent:\n")

	result, err := intent.Check(repoPath)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if result.OK {
		t.Fatalf("expected OK=false for commit with empty Intent trailer, got %+v", result)
	}
	if !strings.Contains(strings.ToLower(result.Message), "intent") {
		t.Fatalf("expected message to name the missing intent, got %q", result.Message)
	}
}

func TestCheck_PresentIntentProceeds(t *testing.T) {
	repoPath := initRepoWithCommit(t, "Add feature\n\nIntent: Add a login button to the nav bar\n")

	result, err := intent.Check(repoPath)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !result.OK {
		t.Fatalf("expected OK=true for commit with a non-empty Intent trailer, got %+v", result)
	}
	if result.Message == "" {
		t.Fatalf("expected a non-empty success message")
	}
}
