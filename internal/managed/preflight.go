package managed

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/douglasjarquin/made/internal/safegit"
)

var fullSHARegexp = regexp.MustCompile(`^[0-9a-f]{40}$`)
var policyHashRegexp = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

// PreflightResult holds the verified inputs produced by preflight.
type PreflightResult struct {
	// ConfigBytes are the exact bytes read from the trusted config file.
	// These must be used for parsing; the file must not be re-read.
	ConfigBytes []byte
}

// ValidateOptions performs pure format/argument validation.
// Returns a non-nil error for usage errors that should produce exit 2 (no events).
func ValidateOptions(opts *Options) error {
	if opts.RunID == "" {
		return fmt.Errorf("--run-id is required")
	}
	if opts.MissionID == "" {
		return fmt.Errorf("--mission-id is required")
	}
	if !filepath.IsAbs(opts.Workspace) {
		return fmt.Errorf("--workspace %q must be an absolute path", opts.Workspace)
	}
	if !filepath.IsAbs(opts.TrustedConfig) {
		return fmt.Errorf("--trusted-config %q must be an absolute path", opts.TrustedConfig)
	}
	if !filepath.IsAbs(opts.EvidenceDir) {
		return fmt.Errorf("--evidence-dir %q must be an absolute path", opts.EvidenceDir)
	}
	if !fullSHARegexp.MatchString(opts.InputSHA) {
		return fmt.Errorf("--input-sha %q must be a full 40-hex commit SHA", opts.InputSHA)
	}
	if !fullSHARegexp.MatchString(opts.BaseSHA) {
		return fmt.Errorf("--base-sha %q must be a full 40-hex commit SHA", opts.BaseSHA)
	}
	if !policyHashRegexp.MatchString(opts.PolicyHash) {
		return fmt.Errorf("--policy-hash %q must match sha256:<64-lowercase-hex>", opts.PolicyHash)
	}
	if opts.DecisionsPath != "" && !filepath.IsAbs(opts.DecisionsPath) {
		return fmt.Errorf("--decisions %q must be an absolute path", opts.DecisionsPath)
	}
	return nil
}

// RunPreflight verifies all preconditions before any validation stage begins.
// On success it returns a PreflightResult containing the verified config bytes.
// Format/argument validation is handled separately by ValidateOptions; this
// function performs OS and Git checks only.
func RunPreflight(ctx context.Context, opts *Options) (PreflightResult, error) {
	// workspace exists and is a Git working tree
	wsInfo, err := os.Stat(opts.Workspace)
	if err != nil {
		return PreflightResult{}, fmt.Errorf("preflight: workspace %q: %w", opts.Workspace, err)
	}
	if !wsInfo.IsDir() {
		return PreflightResult{}, fmt.Errorf("preflight: workspace %q is not a directory", opts.Workspace)
	}
	if _, gitErr := safegit.Output(ctx, safegit.Command{
		WorktreePath: opts.Workspace,
		Args:         []string{"rev-parse", "--git-dir"},
	}); gitErr != nil {
		return PreflightResult{}, fmt.Errorf("preflight: workspace %q is not a Git working tree: %w", opts.Workspace, gitErr)
	}

	// HEAD^{commit} exactly equals input_sha
	headSHA, err := safegit.Output(ctx, safegit.Command{
		WorktreePath: opts.Workspace,
		Args:         []string{"rev-parse", "--verify", "HEAD^{commit}"},
	})
	if err != nil {
		return PreflightResult{}, fmt.Errorf("preflight: resolve HEAD: %w", err)
	}
	if headSHA != opts.InputSHA {
		return PreflightResult{}, fmt.Errorf("preflight: workspace HEAD is %q but input_sha is %q", headSHA, opts.InputSHA)
	}

	// both commits exist locally
	if _, err := safegit.Output(ctx, safegit.Command{
		WorktreePath: opts.Workspace,
		Args:         []string{"cat-file", "-e", opts.InputSHA + "^{commit}"},
	}); err != nil {
		return PreflightResult{}, fmt.Errorf("preflight: input_sha %q does not exist locally: %w", opts.InputSHA, err)
	}
	if _, err := safegit.Output(ctx, safegit.Command{
		WorktreePath: opts.Workspace,
		Args:         []string{"cat-file", "-e", opts.BaseSHA + "^{commit}"},
	}); err != nil {
		return PreflightResult{}, fmt.Errorf("preflight: base_sha %q does not exist locally: %w", opts.BaseSHA, err)
	}

	// base_sha is an ancestor of input_sha
	mergeBase, err := safegit.Output(ctx, safegit.Command{
		WorktreePath: opts.Workspace,
		Args:         []string{"merge-base", opts.BaseSHA, opts.InputSHA},
	})
	if err != nil {
		return PreflightResult{}, fmt.Errorf("preflight: compute merge-base: %w", err)
	}
	if mergeBase != opts.BaseSHA {
		return PreflightResult{}, fmt.Errorf("preflight: base_sha %q is not an ancestor of input_sha %q", opts.BaseSHA, opts.InputSHA)
	}

	// worktree is clean (no tracked or non-ignored untracked changes)
	status, err := safegit.Output(ctx, safegit.Command{
		WorktreePath: opts.Workspace,
		Args:         []string{"status", "--porcelain", "--untracked-files=all"},
	})
	if err != nil {
		return PreflightResult{}, fmt.Errorf("preflight: inspect worktree status: %w", err)
	}
	if status != "" {
		return PreflightResult{}, fmt.Errorf("preflight: workspace has uncommitted changes:\n%s", status)
	}

	// trusted-config: read exactly once, verify it is a regular file even after open
	// (closes the symlink-swap race between Lstat and Open).
	configInfo, err := os.Lstat(opts.TrustedConfig)
	if err != nil {
		return PreflightResult{}, fmt.Errorf("preflight: trusted-config stat: %w", err)
	}
	if !configInfo.Mode().IsRegular() {
		return PreflightResult{}, fmt.Errorf("preflight: trusted-config %q is not a regular file", opts.TrustedConfig)
	}
	var configBytes []byte
	if err := func() error {
		f, err := os.Open(opts.TrustedConfig)
		if err != nil {
			return fmt.Errorf("open: %w", err)
		}
		defer func() { _ = f.Close() }()
		// Verify fd points to a regular file (prevents symlink-swap race between Lstat and Open).
		fi, err := f.Stat()
		if err != nil {
			return fmt.Errorf("fstat: %w", err)
		}
		if !fi.Mode().IsRegular() {
			return fmt.Errorf("not a regular file after open")
		}
		configBytes, err = io.ReadAll(f)
		return err
	}(); err != nil {
		return PreflightResult{}, fmt.Errorf("preflight: read trusted-config: %w", err)
	}

	// verify SHA-256 of config bytes matches policy_hash
	sum := sha256.Sum256(configBytes)
	computedHash := "sha256:" + hex.EncodeToString(sum[:])
	if computedHash != opts.PolicyHash {
		return PreflightResult{}, fmt.Errorf("preflight: trusted-config hash mismatch: computed %s, expected %s", computedHash, opts.PolicyHash)
	}

	// evidence-dir is outside the workspace, using canonical path resolution.
	canonicalWS, err := filepath.EvalSymlinks(opts.Workspace)
	if err != nil {
		return PreflightResult{}, fmt.Errorf("preflight: resolve workspace canonical path: %w", err)
	}
	evClean := filepath.Clean(opts.EvidenceDir)
	wsSep := canonicalWS + string(filepath.Separator)
	if strings.HasPrefix(evClean+string(filepath.Separator), wsSep) || evClean == canonicalWS {
		return PreflightResult{}, fmt.Errorf("preflight: evidence-dir %q must be outside the workspace %q", opts.EvidenceDir, opts.Workspace)
	}

	return PreflightResult{ConfigBytes: configBytes}, nil
}

// CaptureWorktreeState captures HEAD and porcelain status for nonmutation verification.
func CaptureWorktreeState(ctx context.Context, workspace string) (head, status string, err error) {
	head, err = safegit.Output(ctx, safegit.Command{
		WorktreePath: workspace,
		Args:         []string{"rev-parse", "HEAD"},
	})
	if err != nil {
		return "", "", fmt.Errorf("capture worktree state: HEAD: %w", err)
	}
	status, err = safegit.Output(ctx, safegit.Command{
		WorktreePath: workspace,
		Args:         []string{"status", "--porcelain", "--untracked-files=all"},
	})
	if err != nil {
		return "", "", fmt.Errorf("capture worktree state: status: %w", err)
	}
	return head, status, nil
}

// VerifyWorktreeUnchanged checks that HEAD and status are identical to the captured values.
func VerifyWorktreeUnchanged(ctx context.Context, workspace, beforeHead, beforeStatus string) error {
	afterHead, afterStatus, err := CaptureWorktreeState(ctx, workspace)
	if err != nil {
		return err
	}
	if afterHead != beforeHead {
		return fmt.Errorf("workspace HEAD changed during stage: before=%s after=%s", beforeHead, afterHead)
	}
	if afterStatus != beforeStatus {
		return fmt.Errorf("workspace status changed during stage: before=%q after=%q", beforeStatus, afterStatus)
	}
	return nil
}

// VerifyExactInputSHA checks that HEAD == inputSHA and workspace is clean.
// This guard prevents undetected mutations or concurrent workspace changes.
func VerifyExactInputSHA(ctx context.Context, workspace, inputSHA string) error {
	head, status, err := CaptureWorktreeState(ctx, workspace)
	if err != nil {
		return err
	}
	if head != inputSHA {
		return fmt.Errorf("workspace HEAD %s does not match input_sha %s", head, inputSHA)
	}
	if status != "" {
		return fmt.Errorf("workspace not clean (dirty files detected)")
	}
	return nil
}
