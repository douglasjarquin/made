package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/douglasjarquin/made/internal/skill"
)

func main() {
	rel := filepath.Join("skills", skill.Name, "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(rel), 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "genskill: mkdir: %v\n", err)
		os.Exit(1)
	}
	if err := os.WriteFile(rel, []byte(skill.Markdown()), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "genskill: write %s: %v\n", rel, err)
		os.Exit(1)
	}
	fmt.Printf("genskill: wrote %s\n", rel)
}
