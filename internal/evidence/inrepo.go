package evidence

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
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
	repoPath, err := filepath.EvalSymlinks(s.RepoPath)
	if err != nil {
		return fmt.Errorf("evidence: resolve repository path: %w", err)
	}
	evidenceRoot := filepath.Join(repoPath, dir)
	if !isContainedPath(repoPath, evidenceRoot) {
		return fmt.Errorf("evidence: configured directory %q escapes repository", dir)
	}
	runDir := filepath.Join(evidenceRoot, runID)
	if !isContainedPath(evidenceRoot, runDir) {
		return fmt.Errorf("evidence: run ID %q escapes evidence directory", runID)
	}
	if err := ensureEvidenceDirectory(repoPath); err != nil {
		return err
	}
	if err := ensureEvidenceDirectory(runDir); err != nil {
		return err
	}
	for name, data := range files {
		dest := filepath.Join(runDir, name)
		rel, err := filepath.Rel(runDir, dest)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(name) {
			return fmt.Errorf("evidence: path %q escapes run evidence directory", name)
		}
		if err := ensureEvidenceDirectory(filepath.Dir(dest)); err != nil {
			return fmt.Errorf("evidence: create evidence dir for %q: %w", name, err)
		}
		file, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_TRUNC|unix.O_NOFOLLOW, 0o644)
		if err != nil {
			return fmt.Errorf("evidence: write evidence file %q: %w", name, err)
		}
		if _, err := file.Write(Redact(data)); err != nil {
			_ = file.Close()
			return fmt.Errorf("evidence: write evidence file %q: %w", name, err)
		}
		if err := file.Close(); err != nil {
			return fmt.Errorf("evidence: close evidence file %q: %w", name, err)
		}
	}
	return nil
}

func isContainedPath(root, target string) bool {
	rel, err := filepath.Rel(root, target)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

func ensureEvidenceDirectory(path string) error {
	clean := filepath.Clean(path)
	volume := filepath.VolumeName(clean)
	rest := strings.TrimPrefix(clean, volume)
	current := volume
	if strings.HasPrefix(rest, string(filepath.Separator)) {
		current += string(filepath.Separator)
		rest = strings.TrimPrefix(rest, string(filepath.Separator))
	}
	for _, component := range strings.Split(rest, string(filepath.Separator)) {
		if component == "" {
			continue
		}
		current = filepath.Join(current, component)
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			if err := os.Mkdir(current, 0o755); err != nil && !errors.Is(err, os.ErrExist) {
				return fmt.Errorf("evidence: create directory %q: %w", current, err)
			}
			info, err = os.Lstat(current)
		}
		if err != nil {
			return fmt.Errorf("evidence: inspect directory %q: %w", current, err)
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return fmt.Errorf("evidence: refusing unsafe directory %q", current)
		}
	}
	return nil
}
