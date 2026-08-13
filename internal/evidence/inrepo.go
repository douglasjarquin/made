package evidence

import (
	"fmt"
	"os"
	"path/filepath"
)

type InRepoStore struct {
	RepoPath string
	Dir      string
}

func (s *InRepoStore) WriteEvidence(runID string, files map[string][]byte) error {
	if runID == "" {
		return fmt.Errorf("evidence: runID must not be empty")
	}
	dir := s.Dir
	if dir == "" {
		dir = DefaultDir
	}
	runDir := filepath.Join(s.RepoPath, dir, runID)

	for name, data := range files {
		dest := filepath.Join(runDir, name)
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return fmt.Errorf("evidence: create evidence dir for %q: %w", name, err)
		}
		if err := os.WriteFile(dest, data, 0o644); err != nil {
			return fmt.Errorf("evidence: write evidence file %q: %w", name, err)
		}
	}
	return nil
}
