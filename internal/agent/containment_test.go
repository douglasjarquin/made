package agent

import (
	"slices"
	"testing"
)

func TestBubblewrapReviewArgsAvoidSetuidIncompatibleUserNamespaceFlag(t *testing.T) {
	args := bubblewrapReviewArgs("/bin/agent", []string{"--review"}, "/tmp/review", []string{"/tmp/source"}, []string{"/tmp/mask"})
	if slices.Contains(args, "--unshare-user") || slices.Contains(args, "--unshare-user-try") {
		t.Fatalf("bubblewrap arguments request an incompatible user namespace mode: %v", args)
	}
	if !slices.Contains(args, "--ro-bind") || !slices.Contains(args, "/tmp/mask") {
		t.Fatalf("bubblewrap protected path is not masked with a read-only bind: %v", args)
	}
}
