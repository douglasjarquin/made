package cursor

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"github.com/douglasjarquin/made/internal/config"
)

type DriftKind string

const (
	DriftMissing  DriftKind = "missing"
	DriftStale    DriftKind = "stale"
	DriftShadowed DriftKind = "shadowed"
	DriftShouldGo DriftKind = "should_be_removed"
)

type Drift struct {
	RelPath     string    `json:"path"`
	Kind        DriftKind `json:"kind"`
	Remediation string    `json:"remediation"`
}

func Check(root string, cfg config.Config) ([]Drift, error) {
	files, err := ManagedFiles(cfg)
	if err != nil {
		return nil, err
	}

	var drift []Drift
	for _, f := range files {
		full := filepath.Join(root, filepath.FromSlash(f.RelPath))
		info, statErr := os.Lstat(full)
		exists := statErr == nil

		if f.Content == nil {
			if exists && info.Mode().IsRegular() {
				if data, readErr := os.ReadFile(full); readErr == nil && hasMarker(data) {
					drift = append(drift, Drift{RelPath: f.RelPath, Kind: DriftShouldGo, Remediation: "run `made cursor sync`"})
				}
			}
			continue
		}

		if !exists {
			drift = append(drift, Drift{RelPath: f.RelPath, Kind: DriftMissing, Remediation: "run `made cursor sync`"})
			continue
		}
		if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			drift = append(drift, Drift{RelPath: f.RelPath, Kind: DriftShadowed, Remediation: fmt.Sprintf("%s is not a regular file; move it aside, then run `made cursor sync`", f.RelPath)})
			continue
		}
		data, readErr := os.ReadFile(full)
		if readErr != nil {
			drift = append(drift, Drift{RelPath: f.RelPath, Kind: DriftShadowed, Remediation: readErr.Error()})
			continue
		}
		if !hasMarker(data) {
			drift = append(drift, Drift{RelPath: f.RelPath, Kind: DriftShadowed, Remediation: fmt.Sprintf("%s already exists and is not Made-owned; run `made cursor sync --adopt` to take ownership, or move it aside", f.RelPath)})
			continue
		}
		if !bytes.Equal(data, f.Content) {
			drift = append(drift, Drift{RelPath: f.RelPath, Kind: DriftStale, Remediation: "run `made cursor sync`"})
		}
	}
	return drift, nil
}
