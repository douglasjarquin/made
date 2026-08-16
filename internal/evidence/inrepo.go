package evidence

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type InRepoStore struct {
	RepoPath string
	Dir      string
}

func (s *InRepoStore) Location(runID string) string {
	dir := s.Dir
	if dir == "" {
		dir = DefaultDir
	}
	return filepath.Join(dir, runID)
}

func (s *InRepoStore) WriteEvidence(runID string, files map[string][]byte) error {
	if err := validateEvidenceInput(runID, files); err != nil {
		return err
	}
	dir := s.Dir
	if dir == "" {
		dir = DefaultDir
	}
	runDir := filepath.Join(s.RepoPath, dir, runID)
	for name, data := range files {
		dest := filepath.Join(runDir, name)
		rel, err := filepath.Rel(runDir, dest)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(name) {
			return fmt.Errorf("evidence: path %q escapes run evidence directory", name)
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return fmt.Errorf("evidence: create evidence dir for %q: %w", name, err)
		}
		if err := os.WriteFile(dest, Redact(data), 0o644); err != nil {
			return fmt.Errorf("evidence: write evidence file %q: %w", name, err)
		}
	}
	return nil
}
