package cursor

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

type WriteAction string

const (
	ActionCreated   WriteAction = "created"
	ActionUpdated   WriteAction = "updated"
	ActionUnchanged WriteAction = "unchanged"
	ActionAdopted   WriteAction = "adopted"
	ActionRemoved   WriteAction = "removed"
)

type WriteResult struct {
	RelPath string      `json:"path"`
	Action  WriteAction `json:"action"`
}

type CollisionError struct {
	RelPath string
}

func (e *CollisionError) Error() string {
	return fmt.Sprintf("cursor: %s already exists and is not Made-owned; rerun with --adopt to take ownership, or move it aside first", e.RelPath)
}

func writeManagedFile(root, relPath string, content []byte, adopt bool) (WriteResult, error) {
	full := filepath.Join(root, filepath.FromSlash(relPath))
	info, err := os.Lstat(full)
	if errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return WriteResult{}, fmt.Errorf("cursor: create %s: %w", filepath.Dir(relPath), err)
		}
		if err := atomicWrite(full, content, 0o644); err != nil {
			return WriteResult{}, fmt.Errorf("cursor: write %s: %w", relPath, err)
		}
		return WriteResult{RelPath: relPath, Action: ActionCreated}, nil
	}
	if err != nil {
		return WriteResult{}, fmt.Errorf("cursor: inspect %s: %w", relPath, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return WriteResult{}, fmt.Errorf("cursor: %s is a symlink; refusing to write through it", relPath)
	}
	if !info.Mode().IsRegular() {
		return WriteResult{}, fmt.Errorf("cursor: %s is not a regular file", relPath)
	}

	existing, err := os.ReadFile(full)
	if err != nil {
		return WriteResult{}, fmt.Errorf("cursor: read %s: %w", relPath, err)
	}
	owned := hasMarker(existing)
	if !owned && !adopt {
		return WriteResult{}, &CollisionError{RelPath: relPath}
	}
	if bytes.Equal(existing, content) {
		return WriteResult{RelPath: relPath, Action: ActionUnchanged}, nil
	}
	if err := atomicWrite(full, content, info.Mode().Perm()); err != nil {
		return WriteResult{}, fmt.Errorf("cursor: write %s: %w", relPath, err)
	}
	action := ActionUpdated
	if !owned {
		action = ActionAdopted
	}
	return WriteResult{RelPath: relPath, Action: action}, nil
}

func removeManagedFileIfOwned(root, relPath string) (WriteResult, error) {
	full := filepath.Join(root, filepath.FromSlash(relPath))
	info, err := os.Lstat(full)
	if errors.Is(err, os.ErrNotExist) {
		return WriteResult{RelPath: relPath, Action: ActionUnchanged}, nil
	}
	if err != nil {
		return WriteResult{}, fmt.Errorf("cursor: inspect %s: %w", relPath, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return WriteResult{RelPath: relPath, Action: ActionUnchanged}, nil
	}
	data, err := os.ReadFile(full)
	if err != nil {
		return WriteResult{}, fmt.Errorf("cursor: read %s: %w", relPath, err)
	}
	if !hasMarker(data) {
		return WriteResult{RelPath: relPath, Action: ActionUnchanged}, nil
	}
	if err := os.Remove(full); err != nil {
		return WriteResult{}, fmt.Errorf("cursor: remove %s: %w", relPath, err)
	}
	return WriteResult{RelPath: relPath, Action: ActionRemoved}, nil
}

func atomicWrite(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".made-cursor-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	_, writeErr := tmp.Write(data)
	closeErr := tmp.Close()
	if writeErr != nil {
		_ = os.Remove(tmpPath)
		return writeErr
	}
	if closeErr != nil {
		_ = os.Remove(tmpPath)
		return closeErr
	}
	if err := os.Chmod(tmpPath, perm); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	return nil
}
