package verify_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/douglasjarquin/made/internal/managed"
	"github.com/douglasjarquin/made/internal/verify"
)

func TestRun_ProducesReceiptQueryableByStatusAndSHA(t *testing.T) {
	dir, _, inputSHA := newTestRepo(t, ".made.yaml", testConfigNoAgent)

	out, err := verify.Run(context.Background(), verify.RunParams{WorkDir: dir, BaseRef: "origin/main"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out.Receipt.Outcome != managed.OutcomePassed {
		t.Fatalf("Outcome = %q, want passed", out.Receipt.Outcome)
	}
	if out.Receipt.InputSHA != inputSHA {
		t.Fatalf("Receipt.InputSHA = %q, want %q", out.Receipt.InputSHA, inputSHA)
	}

	status, err := verify.StatusHead(context.Background(), dir)
	if err != nil {
		t.Fatalf("StatusHead: %v", err)
	}
	if status.Receipt == nil {
		t.Fatal("StatusHead: expected a receipt for current HEAD")
	}
	if status.Receipt.InputSHA != inputSHA {
		t.Errorf("StatusHead receipt InputSHA = %q, want %q", status.Receipt.InputSHA, inputSHA)
	}

	r, ok, err := verify.ReceiptForSHA(context.Background(), dir, inputSHA)
	if err != nil {
		t.Fatalf("ReceiptForSHA: %v", err)
	}
	if !ok {
		t.Fatal("ReceiptForSHA: expected a receipt to be found")
	}
	if r.Outcome != managed.OutcomePassed {
		t.Errorf("ReceiptForSHA outcome = %q, want passed", r.Outcome)
	}
}

func TestRun_EarlierReceiptNeverCoversLaterCommit(t *testing.T) {
	dir, _, firstSHA := newTestRepo(t, ".made.yaml", testConfigNoAgent)

	if _, err := verify.Run(context.Background(), verify.RunParams{WorkDir: dir, BaseRef: "origin/main"}); err != nil {
		t.Fatalf("first Run: %v", err)
	}

	writeTestFile(t, filepath.Join(dir, "more.go"), "package main\n")
	gitAt(t, dir, "add", ".")
	gitAt(t, dir, "commit", "-m", "second commit")
	secondSHA := gitAt(t, dir, "rev-parse", "HEAD")

	if _, err := verify.Run(context.Background(), verify.RunParams{WorkDir: dir, BaseRef: "origin/main"}); err != nil {
		t.Fatalf("second Run: %v", err)
	}

	_, firstStillFound, err := verify.ReceiptForSHA(context.Background(), dir, firstSHA)
	if err != nil {
		t.Fatalf("ReceiptForSHA(first): %v", err)
	}
	if !firstStillFound {
		t.Error("expected the first commit's receipt to remain retrievable by its own exact SHA")
	}

	second, ok, err := verify.ReceiptForSHA(context.Background(), dir, secondSHA)
	if err != nil {
		t.Fatalf("ReceiptForSHA(second): %v", err)
	}
	if !ok || second.InputSHA != secondSHA {
		t.Fatalf("expected a distinct receipt for the second commit, got ok=%v receipt=%+v", ok, second)
	}
}

func TestRun_DirtyWorktreeSurfacesResolutionError(t *testing.T) {
	dir, _, _ := newTestRepo(t, ".made.yaml", testConfigNoAgent)
	writeTestFile(t, filepath.Join(dir, "dirty.txt"), "x\n")

	_, err := verify.Run(context.Background(), verify.RunParams{WorkDir: dir, BaseRef: "origin/main"})
	if err == nil {
		t.Fatal("expected an error for a dirty worktree")
	}
}

func TestClean_RemovesState(t *testing.T) {
	dir, _, inputSHA := newTestRepo(t, ".made.yaml", testConfigNoAgent)
	if _, err := verify.Run(context.Background(), verify.RunParams{WorkDir: dir, BaseRef: "origin/main"}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if _, ok, _ := verify.ReceiptForSHA(context.Background(), dir, inputSHA); !ok {
		t.Fatal("expected a receipt before Clean")
	}

	if _, err := verify.Clean(context.Background(), dir); err != nil {
		t.Fatalf("Clean: %v", err)
	}

	_, ok, err := verify.ReceiptForSHA(context.Background(), dir, inputSHA)
	if err != nil {
		t.Fatalf("ReceiptForSHA after Clean: %v", err)
	}
	if ok {
		t.Fatal("expected no receipt to remain after Clean")
	}
}
