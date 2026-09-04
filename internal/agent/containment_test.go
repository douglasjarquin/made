package agent

import (
	"slices"
	"strings"
	"testing"
)

func TestBubblewrapReviewArgsAvoidSetuidIncompatibleUserNamespaceFlag(t *testing.T) {
	args := bubblewrapReviewArgs("/bin/agent", []string{"--review"}, "/tmp/review", []string{"/tmp/source"}, []string{"/tmp/mask"}, nil)
	if slices.Contains(args, "--unshare-user") || slices.Contains(args, "--unshare-user-try") {
		t.Fatalf("bubblewrap arguments request an incompatible user namespace mode: %v", args)
	}
	if !slices.Contains(args, "--ro-bind") || !slices.Contains(args, "/tmp/mask") {
		t.Fatalf("bubblewrap protected path is not masked with a read-only bind: %v", args)
	}
}

func TestBubblewrapReviewArgsKeepHarnessStateWritableButSourceMasked(t *testing.T) {
	args := bubblewrapReviewArgs("/bin/agent", []string{"--review"}, "/tmp/review", []string{"/tmp/source"}, []string{"/tmp/mask"}, []string{"/home/u/.claude"})
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "--bind /home/u/.claude /home/u/.claude") {
		t.Fatalf("harness state directory is not bound writable: %v", args)
	}
	if !strings.Contains(joined, "--ro-bind /tmp/mask /tmp/source") {
		t.Fatalf("candidate source is not masked read-only: %v", args)
	}
	if strings.Index(joined, "--bind /home/u/.claude") > strings.Index(joined, "--ro-bind /tmp/mask") {
		t.Fatalf("source mask must be applied after state binds so it wins on overlap: %v", args)
	}
}

func TestEveryKindDeclaresStateDirs(t *testing.T) {
	for _, kind := range SupportedKinds() {
		if len(kind.stateDirs()) == 0 {
			t.Fatalf("%s declares no state directories; Linux containment would leave it unable to write session state", kind)
		}
	}
}
