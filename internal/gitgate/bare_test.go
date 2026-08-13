package gitgate_test

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/douglasjarquin/made/internal/gitgate"
)

func TestInitBareCreatesBareRepository(t *testing.T) {
	repoPath := filepath.Join(t.TempDir(), "gate.git")

	if err := gitgate.InitBare(repoPath); err != nil {
		t.Fatalf("InitBare(%q) returned error: %v", repoPath, err)
	}

	out, err := exec.Command("git", "-C", repoPath, "rev-parse", "--is-bare-repository").Output()
	if err != nil {
		t.Fatalf("git rev-parse --is-bare-repository failed: %v", err)
	}
	if got := strings.TrimSpace(string(out)); got != "true" {
		t.Fatalf("expected bare repository, git reported %q", got)
	}
}

func TestInitBareRejectsEmptyPath(t *testing.T) {
	if err := gitgate.InitBare(""); err == nil {
		t.Fatal("expected InitBare(\"\") to return an error")
	}
}
