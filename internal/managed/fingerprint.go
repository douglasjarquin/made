package managed

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
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
// For managed mode, the fingerprint is based on structural identity:
// stage, kind, code, class, normalized paths, and symbol. The description
// is intentionally excluded to provide stability across paraphrasing, but
// this requires structural fields to be present. Managed findings without
// stable structural identity will be rejected at preflight.
//
// The format is: sha256:<64-lowercase-hex>
func Fingerprint(in FingerprintInput) string {
	// Managed mode always uses structural fingerprinting.
	// Description is never used (even if all structural fields are empty).
	parts := []string{
		fingerprintVersion,
		in.Stage,
		in.Kind,
		in.Code,
		in.Class,
		normalizePaths(in.Paths, in.WorkspacePrefix),
		in.Symbol,
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

// ValidateStableFindingIdentity checks that a finding has sufficient structural
// identity for managed validation. Managed findings must include stable fields
// to enable safe Decision binding across reruns with paraphrased descriptions.
//
// Requirements:
// - code: must be a non-empty, finding-specific identifier (e.g. "review.security_issue",
//         not a generic category like "review.issue"). This ensures unique findings on
//         the same file can be distinguished.
// - class: must be a non-empty category (e.g. "security", "style", "architecture")
// - paths: must contain at least one repository-relative path. Paths are validated to be:
//          * repository-relative (not absolute)
//          * clean (no redundant separators, no ".")
//          * free of path-escape sequences ("../")
// - symbol/locus: strongly recommended when applicable (e.g., "function name")
//
// A finding without these fields cannot be safely bound to a Decision and will
// be rejected to prevent ambiguous decision application or unintended path escapes.
func ValidateStableFindingIdentity(in FingerprintInput) error {
	if in.Code == "" {
		return fmt.Errorf("finding missing required 'code' field (stable, finding-specific identifier like 'review.sql_injection', not generic 'review.issue')")
	}
	if in.Class == "" {
		return fmt.Errorf("finding missing required 'class' field (stable category like 'security', 'style', 'architecture')")
	}
	if len(in.Paths) == 0 {
		return fmt.Errorf("finding missing required 'paths' field (one or more repository-relative paths)")
	}

	// Validate paths: must be relative, clean, and free of escape sequences.
	for i, p := range in.Paths {
		if p == "" {
			return fmt.Errorf("finding path[%d] is empty", i)
		}
		if filepath.IsAbs(p) {
			return fmt.Errorf("finding path[%d] is absolute: %q (must be repository-relative)", i, p)
		}

		// Clean the path and check for modifications that indicate issues.
		clean := filepath.Clean(p)
		if clean != p {
			return fmt.Errorf("finding path[%d] is not clean: %q → %q", i, p, clean)
		}

		// Reject paths with ".." or "." components.
		if strings.Contains(p, "..") {
			return fmt.Errorf("finding path[%d] contains path-escape sequence '..': %q", i, p)
		}
		if p == "." || strings.HasPrefix(p, "./") || strings.HasSuffix(p, "/.") || strings.Contains(p, "/./") {
			return fmt.Errorf("finding path[%d] contains invalid '.' component: %q", i, p)
		}
	}

	return nil
}
