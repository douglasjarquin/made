package managed

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/douglasjarquin/made/internal/config"
)

const (
	maxGuideCount          = config.MaxReviewGuides
	maxGuidePathBytes      = config.MaxReviewGuidePathBytes
	maxGuideBytes          = 1 << 20
	maxAggregateGuideBytes = 4 << 20
	maxConsultedGuideCount = maxGuideCount
)

// GuideBinding is one configured guide's identity as resolved from the
// trusted base at invocation time (project issue #40): its
// repository-relative path, exact content hash, and byte count. Made binds
// every configured guide this way before Review runs, so a result cannot
// silently be produced against different guide content.
type GuideBinding struct {
	Path        string `json:"path"`
	ContentHash string `json:"content_hash"`
	Bytes       int    `json:"bytes"`
}

// GuideEvidence is the review stage's truthful, bounded record of guide
// provenance (project issue #40): the trusted guides Made configured for
// this run, and whatever the reviewer optionally reported consulting. It
// never claims proof that a reviewer understood the prose.
type GuideEvidence struct {
	Configured []GuideBinding   `json:"configured"`
	Consulted  []GuideConsulted `json:"consulted,omitempty"`
}

// TrustedGuideRoot derives the repository root that governs relative guide
// paths from the trusted config file's own path, mirroring the directory
// config.Locate treats as root for that config's layout. Managed mode has
// no separate "trusted worktree at a git ref" concept yet - that arrives
// with made verify (issue #41) - so the trusted config's own resolved
// location is the only trusted root available today.
func TrustedGuideRoot(trustedConfigPath string) string {
	dir := filepath.Dir(trustedConfigPath)
	base := filepath.Base(trustedConfigPath)
	if base == config.DirectoryConfigFileName && filepath.Base(dir) == config.DirectoryName {
		return filepath.Dir(dir)
	}
	return dir
}

// ResolveTrustedGuides reads, hashes, and binds every configured guide path
// from root, in the given order. It never follows symlinks, never reads
// outside root, and accepts only regular files.
func ResolveTrustedGuides(root string, guidePaths []string) ([]GuideBinding, error) {
	if len(guidePaths) == 0 {
		return nil, nil
	}
	if len(guidePaths) > maxGuideCount {
		return nil, fmt.Errorf("managed: %d configured guides exceeds the maximum of %d", len(guidePaths), maxGuideCount)
	}

	bindings := make([]GuideBinding, 0, len(guidePaths))
	var aggregate int64
	for _, guidePath := range guidePaths {
		binding, err := resolveOneGuide(root, guidePath)
		if err != nil {
			return nil, err
		}
		aggregate += int64(binding.Bytes)
		if aggregate > maxAggregateGuideBytes {
			return nil, fmt.Errorf("managed: configured guides exceed the aggregate limit of %d bytes", maxAggregateGuideBytes)
		}
		bindings = append(bindings, binding)
	}
	return bindings, nil
}

func resolveOneGuide(root, guidePath string) (GuideBinding, error) {
	if len(guidePath) > maxGuidePathBytes {
		return GuideBinding{}, fmt.Errorf("managed: guide path %q exceeds %d bytes", guidePath, maxGuidePathBytes)
	}
	if path.IsAbs(guidePath) {
		return GuideBinding{}, fmt.Errorf("managed: guide path %q must be repository-relative", guidePath)
	}
	cleaned := path.Clean(guidePath)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return GuideBinding{}, fmt.Errorf("managed: guide path %q must not escape the repository root", guidePath)
	}

	full := filepath.Join(root, filepath.FromSlash(cleaned))
	info, err := os.Lstat(full)
	if err != nil {
		return GuideBinding{}, fmt.Errorf("managed: guide %q is missing in the trusted base: %w", guidePath, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return GuideBinding{}, fmt.Errorf("managed: guide %q must not be a symlink", guidePath)
	}
	if !info.Mode().IsRegular() {
		return GuideBinding{}, fmt.Errorf("managed: guide %q must be a regular file", guidePath)
	}

	f, err := os.Open(full)
	if err != nil {
		return GuideBinding{}, fmt.Errorf("managed: open guide %q: %w", guidePath, err)
	}
	defer func() { _ = f.Close() }()

	stat, err := f.Stat()
	if err != nil {
		return GuideBinding{}, fmt.Errorf("managed: stat guide %q: %w", guidePath, err)
	}
	if !stat.Mode().IsRegular() {
		return GuideBinding{}, fmt.Errorf("managed: guide %q must be a regular file", guidePath)
	}
	if stat.Size() > maxGuideBytes {
		return GuideBinding{}, fmt.Errorf("managed: guide %q exceeds %d bytes", guidePath, maxGuideBytes)
	}

	data, err := io.ReadAll(io.LimitReader(f, maxGuideBytes+1))
	if err != nil {
		return GuideBinding{}, fmt.Errorf("managed: read guide %q: %w", guidePath, err)
	}
	if len(data) > maxGuideBytes {
		return GuideBinding{}, fmt.Errorf("managed: guide %q exceeds %d bytes", guidePath, maxGuideBytes)
	}

	sum := sha256.Sum256(data)
	return GuideBinding{
		Path:        cleaned,
		ContentHash: "sha256:" + hex.EncodeToString(sum[:]),
		Bytes:       len(data),
	}, nil
}
