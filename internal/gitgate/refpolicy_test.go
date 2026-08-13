package gitgate_test

import (
	"strings"
	"testing"

	"github.com/douglasjarquin/made/internal/gitgate"
)

const testSHA = "a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d"
const testNewSHA = "0102030405060708090a0b0c0d0e0f101112131"
const zeroSHA = "0000000000000000000000000000000000000000"

func TestClassifyRefAcceptsNonDefaultBranch(t *testing.T) {
	decision := gitgate.ClassifyRef("refs/heads/feature-x", "main", testSHA, testNewSHA)

	if !decision.Accept {
		t.Fatalf("expected feature branch push to be accepted, got %+v", decision)
	}
	if !decision.CreateRun {
		t.Fatalf("expected feature branch push to create a run, got %+v", decision)
	}
}

func TestClassifyRefRejectsDefaultBranch(t *testing.T) {
	decision := gitgate.ClassifyRef("refs/heads/main", "main", testSHA, testNewSHA)

	if decision.Accept {
		t.Fatalf("expected default branch push to be rejected, got %+v", decision)
	}
	if decision.Message != "pushing the default branch to the gate is not a supported flow" {
		t.Fatalf("unexpected rejection message: %q", decision.Message)
	}
}

func TestClassifyRefRejectsTags(t *testing.T) {
	decision := gitgate.ClassifyRef("refs/tags/v1", "main", testSHA, testNewSHA)

	if decision.Accept {
		t.Fatalf("expected tag ref to be rejected, got %+v", decision)
	}
	if !strings.Contains(decision.Message, "refs/tags/v1") {
		t.Fatalf("expected rejection message to name the ref, got %q", decision.Message)
	}
}

func TestClassifyRefRejectsOtherNamespaces(t *testing.T) {
	decision := gitgate.ClassifyRef("refs/notes/commits", "main", testSHA, testNewSHA)

	if decision.Accept {
		t.Fatalf("expected refs/notes ref to be rejected, got %+v", decision)
	}
	if !strings.Contains(decision.Message, "refs/notes/commits") {
		t.Fatalf("expected rejection message to name the ref, got %q", decision.Message)
	}
}

func TestClassifyRefAcceptsDeletionWithoutCreatingRun(t *testing.T) {
	decision := gitgate.ClassifyRef("refs/heads/feature-x", "main", testSHA, zeroSHA)

	if !decision.Accept {
		t.Fatalf("expected a branch deletion to be accepted, got %+v", decision)
	}
	if decision.CreateRun {
		t.Fatalf("expected a branch deletion to not create a run, got %+v", decision)
	}
}
