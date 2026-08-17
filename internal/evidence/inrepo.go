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
	if runID == "" {
		return fmt.Errorf("evidence: runID must not be empty")
	}
	if err := validatePathPart(runID); err != nil {
		return fmt.Errorf("evidence: invalid runID: %w", err)
	}
	dir := s.Dir
	if dir == "" {
		dir = DefaultDir
	}
	runDir := filepath.Join(s.RepoPath, dir, runID)

	for name, data := range files {
		if err := validatePathPart(name); err != nil {
			return fmt.Errorf("evidence: invalid file name %q: %w", name, err)
		}
		dest := filepath.Join(runDir, name)
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return fmt.Errorf("evidence: create evidence dir for %q: %w", name, err)
		}
		if err := writeAtomic(dest, data, 0o644); err != nil {
			return fmt.Errorf("evidence: write evidence file %q: %w", name, err)
		}
	}
	return nil
}

func writeAtomic(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".evidence-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	dirFile, err := os.Open(dir)
	if err != nil {
		return err
	}
	if err := dirFile.Sync(); err != nil {
		_ = dirFile.Close()
		return err
	}
	return dirFile.Close()
}

func validatePathPart(value string) error {
	clean := filepath.Clean(value)
	if value == "" || filepath.IsAbs(value) || clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return fmt.Errorf("path escapes evidence root")
	}
	return nil
}
