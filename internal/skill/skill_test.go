package skill_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/douglasjarquin/made/internal/skill"
)

func TestVerifyCommittedDetectsDrift(t *testing.T) {
	path := filepath.Join(t.TempDir(), "SKILL.md")
	if err := os.WriteFile(path, []byte("this is not the generated body\n"), 0o644); err != nil {
		t.Fatalf("write drifted fixture: %v", err)
	}

	err := skill.VerifyCommitted(path)
	if err == nil {
		t.Fatal("VerifyCommitted: want drift error, got nil")
	}
	if !strings.Contains(err.Error(), path) {
		t.Errorf("VerifyCommitted error %q does not name the drifted file %q", err, path)
	}
}

func TestVerifyCommittedAcceptsFreshRender(t *testing.T) {
	path := filepath.Join(t.TempDir(), "SKILL.md")
	if err := os.WriteFile(path, []byte(skill.Markdown()), 0o644); err != nil {
		t.Fatalf("write fresh render: %v", err)
	}

	if err := skill.VerifyCommitted(path); err != nil {
		t.Errorf("VerifyCommitted on a fresh render: %v", err)
	}
}

func TestMarkdownIsDeterministic(t *testing.T) {
	first := skill.Markdown()
	second := skill.Markdown()
	if first != second {
		t.Error("Markdown() is not deterministic across calls")
	}
}

// The drift lint lives here (in go test), not in `make lint` (golangci-lint
// cannot see file-content drift), so this test is what actually enforces it.
func TestCommittedSkillFileMatchesGenerator(t *testing.T) {
	path := filepath.Join("..", "..", "skills", "made", "SKILL.md")
	if err := skill.VerifyCommitted(path); err != nil {
		t.Fatal(err)
	}
}
