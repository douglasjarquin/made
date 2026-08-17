package evidence

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	execpkg "github.com/douglasjarquin/made/internal/exec"
	"golang.org/x/sys/unix"
)

type InRepoStore struct {
	RepoPath       string
	Dir            string
	RetentionBytes int
}

func (s *InRepoStore) Location(runID string) string {
	dir := s.Dir
	if dir == "" {
		dir = DefaultDir
	}
	return filepath.Join(dir, runID)
}

func (s *InRepoStore) WriteEvidence(runID string, files map[string][]byte) (err error) {
	return s.WriteEvidenceContext(context.Background(), runID, files)
}

func (s *InRepoStore) WriteEvidenceContext(ctx context.Context, runID string, files map[string][]byte) (err error) {
	if err := validateEvidenceInput(runID, files, s.RetentionBytes); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("evidence: write canceled: %w", err)
	}
	dir := s.Dir
	if dir == "" {
		dir = DefaultDir
	}
	if _, err := safePathComponents(dir); err != nil {
		return fmt.Errorf("evidence: invalid directory: %w", err)
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
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("evidence: write canceled: %w", err)
		}
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
		fileFD, openErr := unix.Openat(parentFD, parts[len(parts)-1], unix.O_WRONLY|unix.O_CREAT|unix.O_TRUNC|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0o600)
		if openErr != nil {
			closeEvidenceDirectories(parentOpened)
			return fmt.Errorf("evidence: open evidence file %q: %w", name, openErr)
		}
		if chmodErr := unix.Fchmod(fileFD, 0o600); chmodErr != nil {
			_ = unix.Close(fileFD)
			closeEvidenceDirectories(parentOpened)
			return fmt.Errorf("evidence: restrict evidence file %q: %w", name, chmodErr)
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

func (s *InRepoStore) PublishEvidence(runID string) error {
	return s.PublishEvidenceContext(context.Background(), runID)
}

func (s *InRepoStore) PublishEvidenceContext(ctx context.Context, runID string) error {
	if err := validateEvidenceInput(runID, nil, s.RetentionBytes); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("evidence: publish canceled: %w", err)
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
	runDir := filepath.Join(evidenceRoot, runID)
	if !isContainedPath(repoPath, evidenceRoot) || !isContainedPath(evidenceRoot, runDir) {
		return fmt.Errorf("evidence: configured path escapes repository")
	}
	if _, err := os.Lstat(runDir); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return fmt.Errorf("evidence: inspect run directory: %w", err)
	}
	info, err := os.Lstat(runDir)
	if err != nil {
		return fmt.Errorf("evidence: inspect run directory: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("evidence: run path is not a directory")
	}
	resolvedRoot, err := filepath.EvalSymlinks(evidenceRoot)
	if err != nil {
		return fmt.Errorf("evidence: resolve evidence directory: %w", err)
	}
	if resolvedRoot != evidenceRoot {
		return fmt.Errorf("evidence: refusing symlinked evidence directory")
	}
	resolvedRun, err := filepath.EvalSymlinks(runDir)
	if err != nil {
		return fmt.Errorf("evidence: resolve run directory: %w", err)
	}
	if resolvedRun != runDir {
		return fmt.Errorf("evidence: refusing symlinked run directory")
	}
	if err := sanitizePublishedEvidence(ctx, runDir, s.RetentionBytes); err != nil {
		return err
	}
	relPath := filepath.Join(dir, runID)
	if err := runEvidenceGit(ctx, repoPath, "add", "--", relPath); err != nil {
		return fmt.Errorf("evidence: stage in-repo evidence: %w", err)
	}
	diff, err := execpkg.Run(ctx, execpkg.Command{
		Name:        "git",
		Args:        []string{"diff", "--cached", "--quiet", "--", relPath},
		Dir:         repoPath,
		Timeout:     evidenceGitTimeout,
		OutputLimit: evidenceGitOutputCap,
	})
	if err != nil {
		return fmt.Errorf("evidence: inspect staged evidence: %w", err)
	}
	if diff.ExitCode == 0 {
		return nil
	} else if diff.ExitCode != 1 {
		return fmt.Errorf("evidence: inspect staged evidence failed with exit code %d: %s", diff.ExitCode, RedactString(string(diff.Stdout)+string(diff.Stderr)))
	}
	titleResult, err := execpkg.Run(ctx, execpkg.Command{
		Name:        "git",
		Args:        []string{"log", "-1", "--format=%s"},
		Dir:         repoPath,
		Timeout:     evidenceGitTimeout,
		OutputLimit: evidenceGitOutputCap,
	})
	if err != nil {
		return fmt.Errorf("evidence: derive commit subject: %w", err)
	}
	if titleResult.ExitCode != 0 {
		return fmt.Errorf("evidence: derive commit subject failed with exit code %d: %s", titleResult.ExitCode, RedactString(string(titleResult.Stdout)+string(titleResult.Stderr)))
	}
	title := strings.TrimSpace(string(titleResult.Stdout))
	if title == "" {
		title = "made: publish evidence"
	}
	if err := runEvidenceGit(ctx, repoPath, "-c", "commit.gpgsign=false", "commit", "--only", "-m", title, "--", relPath); err != nil {
		return fmt.Errorf("evidence: commit in-repo evidence: %w", err)
	}
	return nil
}

func runEvidenceGit(ctx context.Context, repoPath string, args ...string) error {
	result, err := execpkg.Run(ctx, execpkg.Command{
		Name: "git",
		Args: args,
		Dir:  repoPath,
		Env: append(os.Environ(),
			"GIT_AUTHOR_NAME=made-evidence",
			"GIT_AUTHOR_EMAIL=made-evidence@localhost",
			"GIT_COMMITTER_NAME=made-evidence",
			"GIT_COMMITTER_EMAIL=made-evidence@localhost",
		),
		Timeout:     evidenceGitTimeout,
		OutputLimit: evidenceGitOutputCap,
	})
	if err != nil {
		return fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	if result.ExitCode != 0 {
		return fmt.Errorf("git %s failed with exit code %d: %s", strings.Join(args, " "), result.ExitCode, RedactString(strings.TrimSpace(string(result.Stdout)+"\n"+string(result.Stderr))))
	}
	return nil
}

func sanitizePublishedEvidence(ctx context.Context, runDir string, retentionBytes int) error {
	if retentionBytes <= 0 {
		retentionBytes = maxEvidenceBytes
	}
	total := 0
	err := filepath.WalkDir(runDir, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return fmt.Errorf("evidence: inspect published path %q: %w", path, walkErr)
		}
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("evidence: publish canceled: %w", err)
		}
		info, err := os.Lstat(path)
		if err != nil {
			return fmt.Errorf("evidence: inspect published path %q: %w", path, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("evidence: refusing symlinked published path %q", path)
		}
		if info.IsDir() {
			return nil
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("evidence: refusing non-regular published path %q", path)
		}
		if info.Size() > maxEvidenceFileBytes {
			return fmt.Errorf("evidence: published file %q exceeds %d bytes", path, maxEvidenceFileBytes)
		}
		data, err := readPublishedEvidence(path)
		if err != nil {
			return fmt.Errorf("evidence: read published file %q: %w", path, err)
		}
		redacted := Redact(data)
		if len(redacted) > maxEvidenceFileBytes || total+len(redacted) > retentionBytes {
			return fmt.Errorf("evidence: published evidence exceeds retention at %q", path)
		}
		if !bytes.Equal(data, redacted) {
			if err := writePublishedEvidence(path, redacted); err != nil {
				return fmt.Errorf("evidence: redact published file %q: %w", path, err)
			}
		}
		total += len(redacted)
		return nil
	})
	if err != nil {
		return err
	}
	return nil
}

func readPublishedEvidence(path string) ([]byte, error) {
	file, err := os.OpenFile(path, os.O_RDONLY|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	data, err := io.ReadAll(io.LimitReader(file, maxEvidenceFileBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxEvidenceFileBytes {
		return nil, fmt.Errorf("file exceeds %d bytes", maxEvidenceFileBytes)
	}
	return data, nil
}

func writePublishedEvidence(path string, data []byte) error {
	fd, err := unix.Open(path, unix.O_WRONLY|unix.O_TRUNC|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0o600)
	if err != nil {
		return err
	}
	if err := unix.Fchmod(fd, 0o600); err != nil {
		_ = unix.Close(fd)
		return err
	}
	writeErr := writeEvidenceFile(fd, data)
	closeErr := unix.Close(fd)
	if writeErr != nil {
		return writeErr
	}
	return closeErr
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
			if mkdirErr := unix.Mkdirat(current, part, 0o700); mkdirErr != nil && !errors.Is(mkdirErr, unix.EEXIST) {
				closeEvidenceDirectories(opened)
				return -1, nil, mkdirErr
			}
			fd, err = unix.Openat(current, part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		}
		if err != nil {
			closeEvidenceDirectories(opened)
			return -1, nil, err
		}
		if err := restrictEvidenceDirectory(fd); err != nil {
			_ = unix.Close(fd)
			closeEvidenceDirectories(opened)
			return -1, nil, err
		}
		opened = append(opened, fd)
		current = fd
	}
	return current, opened, nil
}

func restrictEvidenceDirectory(fd int) error {
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return err
	}
	if stat.Mode&0o077 != 0 {
		return unix.Fchmod(fd, 0o700)
	}
	return nil
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
