package evidence

import (
	"errors"
	"fmt"
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

func (s *InRepoStore) WriteEvidence(runID string, files map[string][]byte) (err error) {
	if err := validateEvidenceInput(runID, files); err != nil {
		return err
	}
	dir := s.Dir
	if dir == "" {
		dir = DefaultDir
	}
	repoPath, err := filepath.Abs(s.RepoPath)
	if err != nil {
		return fmt.Errorf("evidence: resolve repository path: %w", err)
	}
	repoPath, err = filepath.EvalSymlinks(repoPath)
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
	dirParts, err := safePathComponents(dir)
	if err != nil {
		return fmt.Errorf("evidence: invalid directory: %w", err)
	}
	runParts, err := safePathComponents(runID)
	if err != nil {
		return fmt.Errorf("evidence: invalid run ID: %w", err)
	}
	rootFD, err := unix.Open(repoPath, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return fmt.Errorf("evidence: open repository: %w", err)
	}
	defer func() {
		if closeErr := unix.Close(rootFD); closeErr != nil && err == nil {
			err = fmt.Errorf("evidence: close repository: %w", closeErr)
		}
	}()
	runFD, opened, err := openEvidenceDirectory(rootFD, append(dirParts, runParts...))
	if err != nil {
		return fmt.Errorf("evidence: open run directory: %w", err)
	}
	defer closeEvidenceDirectories(opened)

	for name, content := range files {
		parts, err := safePathComponents(name)
		if err != nil {
			return fmt.Errorf("evidence: invalid file path %q: %w", name, err)
		}
		parentFD := runFD
		parentOpened := []int(nil)
		if len(parts) > 1 {
			parentFD, parentOpened, err = openEvidenceDirectory(runFD, parts[:len(parts)-1])
			if err != nil {
				return fmt.Errorf("evidence: open parent for %q: %w", name, err)
			}
		}
		fileFD, openErr := unix.Openat(parentFD, parts[len(parts)-1], unix.O_WRONLY|unix.O_CREAT|unix.O_TRUNC|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0o644)
		if openErr != nil {
			closeEvidenceDirectories(parentOpened)
			return fmt.Errorf("evidence: open evidence file %q: %w", name, openErr)
		}
		redacted := Redact(content)
		writeErr := writeEvidenceFile(fileFD, redacted)
		closeErr := unix.Close(fileFD)
		closeEvidenceDirectories(parentOpened)
		if writeErr != nil {
			return fmt.Errorf("evidence: write evidence file %q: %w", name, writeErr)
		}
		if closeErr != nil {
			return fmt.Errorf("evidence: close evidence file %q: %w", name, closeErr)
		}
	}
	return nil
}

func isContainedPath(root, target string) bool {
	rel, err := filepath.Rel(root, target)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

func safePathComponents(path string) ([]string, error) {
	if path == "" || filepath.IsAbs(path) || filepath.VolumeName(path) != "" {
		return nil, errors.New("path must be relative")
	}
	parts := strings.Split(path, string(filepath.Separator))
	clean := make([]string, 0, len(parts))
	for _, part := range parts {
		if part == "" {
			continue
		}
		if part == "." || part == ".." {
			return nil, errors.New("path traversal is not allowed")
		}
		clean = append(clean, part)
	}
	if len(clean) == 0 {
		return nil, errors.New("path must not be empty")
	}
	return clean, nil
}

func openEvidenceDirectory(rootFD int, parts []string) (int, []int, error) {
	current := rootFD
	opened := make([]int, 0, len(parts))
	for _, part := range parts {
		fd, err := unix.Openat(current, part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		if errors.Is(err, unix.ENOENT) {
			if mkdirErr := unix.Mkdirat(current, part, 0o755); mkdirErr != nil && !errors.Is(mkdirErr, unix.EEXIST) {
				closeEvidenceDirectories(opened)
				return -1, nil, mkdirErr
			}
			fd, err = unix.Openat(current, part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		}
		if err != nil {
			closeEvidenceDirectories(opened)
			return -1, nil, err
		}
		opened = append(opened, fd)
		current = fd
	}
	return current, opened, nil
}

func closeEvidenceDirectories(fds []int) {
	for i := len(fds) - 1; i >= 0; i-- {
		_ = unix.Close(fds[i])
	}
}

func writeEvidenceFile(fd int, data []byte) error {
	for len(data) > 0 {
		n, err := unix.Write(fd, data)
		if err != nil {
			return err
		}
		if n == 0 {
			return errors.New("short write")
		}
		data = data[n:]
	}
	return unix.Fsync(fd)
}
