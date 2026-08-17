package agent

import (
	"slices"
	"testing"
)

func TestBubblewrapReviewArgsAvoidSetuidIncompatibleUserNamespaceFlag(t *testing.T) {
	args := bubblewrapReviewArgs("/bin/agent", []string{"--review"}, "/tmp/review", []string{"/tmp/source"})
	if slices.Contains(args, "--unshare-user") || slices.Contains(args, "--unshare-user-try") {
		t.Fatalf("bubblewrap arguments request an incompatible user namespace mode: %v", args)
	}
	for index, arg := range args {
		if arg == "--tmpfs" && (index == 0 || args[index-1] != "0555") {
			t.Fatalf("bubblewrap protected path is not masked read-only: %v", args)
		}
	}
}
