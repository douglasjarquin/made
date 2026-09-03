package config

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

func Move(root string, to Layout) (from, dest string, err error) {
	if to != LayoutRoot && to != LayoutDirectory {
		return "", "", fmt.Errorf("config: move target must be %q or %q, got %q", LayoutRoot, LayoutDirectory, to)
	}

	loc, err := Locate(root)
	if err != nil {
		return "", "", err
	}
	if loc.Layout == LayoutAbsent {
		return "", "", fmt.Errorf("config: no configuration found under %s to move", root)
	}
	if loc.Layout == to {
		return "", "", fmt.Errorf("config: already using layout %q", to)
	}

	switch to {
	case LayoutRoot:
		dest = rootConfigPath(root)
	case LayoutDirectory:
		dest = directoryConfigPath(root)
	}

	if _, statErr := os.Lstat(dest); statErr == nil {
		return "", "", fmt.Errorf("config: refusing to overwrite existing %s", dest)
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return "", "", fmt.Errorf("config: inspect %s: %w", dest, statErr)
	}

	if to == LayoutDirectory {
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return "", "", fmt.Errorf("config: create %s: %w", filepath.Dir(dest), err)
		}
	}

	if err := moveFile(loc.Path, dest); err != nil {
		return "", "", err
	}
	return loc.Path, dest, nil
}

func moveFile(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	} else if !errors.Is(err, syscall.EXDEV) {
		return fmt.Errorf("config: move %s to %s: %w", src, dst, err)
	}
	return copyThenRemove(src, dst)
}

func copyThenRemove(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("config: read %s for move: %w", src, err)
	}

	f, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return fmt.Errorf("config: create %s for move: %w", dst, err)
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		_ = os.Remove(dst)
		return fmt.Errorf("config: write %s for move: %w", dst, err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(dst)
		return fmt.Errorf("config: fsync %s for move: %w", dst, err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(dst)
		return fmt.Errorf("config: close %s for move: %w", dst, err)
	}

	verify, err := os.ReadFile(dst)
	if err != nil || !bytes.Equal(verify, data) {
		_ = os.Remove(dst)
		return fmt.Errorf("config: verify copied %s failed", dst)
	}

	if err := os.Remove(src); err != nil {
		return fmt.Errorf("config: remove source %s after copy: %w", src, err)
	}
	return nil
}
