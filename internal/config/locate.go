package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

type Layout string

const (
	LayoutRoot      Layout = "root"
	LayoutDirectory Layout = "directory"
	LayoutLegacy    Layout = "legacy"
	LayoutAbsent    Layout = "absent"
	LayoutConflict  Layout = "conflict"
)

const (
	RootConfigFileName      = ".made.yaml"
	DirectoryName           = ".made"
	DirectoryConfigFileName = "config.yaml"
	DirectoryConfigRelPath  = DirectoryName + "/" + DirectoryConfigFileName
	LegacyConfigFileName    = ".made.yml"
)

const legacyDeprecationWarning = "made: .made.yml is a temporary legacy config path; move to .made.yaml or .made/config.yaml"

type Location struct {
	Root    string
	Path    string
	Layout  Layout
	Warning string
}

type ConflictError struct {
	Root  string
	Paths []string
}

func (e *ConflictError) Error() string {
	return fmt.Sprintf("config: more than one configuration file present under %s: %v", e.Root, e.Paths)
}

func AsConflictError(err error, target **ConflictError) bool {
	return errors.As(err, target)
}

func rootConfigPath(root string) string {
	return filepath.Join(root, RootConfigFileName)
}

func directoryConfigPath(root string) string {
	return filepath.Join(root, filepath.FromSlash(DirectoryConfigRelPath))
}

func legacyConfigPath(root string) string {
	return filepath.Join(root, LegacyConfigFileName)
}

func Locate(root string) (Location, error) {
	if err := rejectUnsafeDir(filepath.Join(root, DirectoryName)); err != nil {
		return Location{Root: root, Layout: LayoutConflict}, err
	}

	rootPath := rootConfigPath(root)
	dirPath := directoryConfigPath(root)
	legacyPath := legacyConfigPath(root)

	rootPresent, err := statConfigCandidate(rootPath)
	if err != nil {
		return Location{Root: root, Layout: LayoutConflict}, err
	}
	dirPresent, err := statConfigCandidate(dirPath)
	if err != nil {
		return Location{Root: root, Layout: LayoutConflict}, err
	}
	legacyPresent, err := statConfigCandidate(legacyPath)
	if err != nil {
		return Location{Root: root, Layout: LayoutConflict}, err
	}

	var firstClass []string
	if rootPresent {
		firstClass = append(firstClass, rootPath)
	}
	if dirPresent {
		firstClass = append(firstClass, dirPath)
	}

	if len(firstClass) > 1 {
		return Location{Root: root, Layout: LayoutConflict}, &ConflictError{Root: root, Paths: firstClass}
	}
	if len(firstClass) == 1 && legacyPresent {
		conflict := append(append([]string{}, firstClass...), legacyPath)
		return Location{Root: root, Layout: LayoutConflict}, &ConflictError{Root: root, Paths: conflict}
	}

	switch {
	case rootPresent:
		return Location{Root: root, Path: rootPath, Layout: LayoutRoot}, nil
	case dirPresent:
		return Location{Root: root, Path: dirPath, Layout: LayoutDirectory}, nil
	case legacyPresent:
		return Location{Root: root, Path: legacyPath, Layout: LayoutLegacy, Warning: legacyDeprecationWarning}, nil
	default:
		return Location{Root: root, Layout: LayoutAbsent}, nil
	}
}

func rejectUnsafeDir(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("config: inspect %s: %w", path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("config: %s must not be a symlink", path)
	}
	if !info.IsDir() {
		return fmt.Errorf("config: %s must be a directory", path)
	}
	return nil
}

func statConfigCandidate(path string) (present bool, err error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("config: inspect %s: %w", path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return false, fmt.Errorf("config: %s must not be a symlink", path)
	}
	if !info.Mode().IsRegular() {
		return false, fmt.Errorf("config: %s is not a regular file", path)
	}
	return true, nil
}
