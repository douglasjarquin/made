package managed

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"sort"
	"strings"
)

// fingerprintVersion is the fingerprint protocol version prefix.
// Changing this value invalidates all existing fingerprints.
const fingerprintVersion = "fpv1"

// FingerprintInput holds the normalized components used to compute a fingerprint.
type FingerprintInput struct {
	Stage       string
	Kind        string
	Code        string
	Class       string
	Symbol      string
	Paths       []string
	Description string
	// WorkspacePrefix is stripped from paths and description to avoid
	// leaking absolute workspace paths into fingerprints.
	WorkspacePrefix string
}

// Fingerprint computes a deterministic, stable SHA-256 fingerprint for a finding.
//
// The fingerprint is stable across minor line-number movement but not across
// changes to code, class, kind, symbol, or normalized paths. The format is:
//
//	sha256:<64-lowercase-hex>
func Fingerprint(in FingerprintInput) string {
	parts := []string{
		fingerprintVersion,
		in.Stage,
		in.Code,
		in.Class,
		in.Kind,
		normalizePaths(in.Paths, in.WorkspacePrefix),
		in.Symbol,
		normalizeDescription(in.Description, in.WorkspacePrefix),
	}
	joined := strings.Join(parts, "\x00")
	sum := sha256.Sum256([]byte(joined))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// normalizePaths returns a normalized, sorted, deduplicated string of paths.
// Absolute workspace prefixes are stripped. Path separators are normalized to /.
func normalizePaths(paths []string, workspacePrefix string) string {
	if len(paths) == 0 {
		return ""
	}
	seen := make(map[string]struct{}, len(paths))
	for _, p := range paths {
		clean := stripWorkspacePrefix(filepath.ToSlash(filepath.Clean(p)), workspacePrefix)
		if clean != "" {
			seen[clean] = struct{}{}
		}
	}
	normalized := make([]string, 0, len(seen))
	for p := range seen {
		normalized = append(normalized, p)
	}
	sort.Strings(normalized)
	return strings.Join(normalized, "|")
}

// normalizeDescription strips the workspace prefix and collapses whitespace.
func normalizeDescription(description, workspacePrefix string) string {
	d := description
	if workspacePrefix != "" {
		d = strings.ReplaceAll(d, workspacePrefix, "")
	}
	// Collapse consecutive whitespace.
	fields := strings.Fields(d)
	return strings.Join(fields, " ")
}

func stripWorkspacePrefix(path, workspacePrefix string) string {
	if workspacePrefix == "" {
		return path
	}
	prefix := filepath.ToSlash(filepath.Clean(workspacePrefix))
	if !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}
	if strings.HasPrefix(path, prefix) {
		return path[len(prefix):]
	}
	return path
}
