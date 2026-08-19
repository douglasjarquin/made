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

// RunPreflight verifies all preconditions before any validation stage begins.
// On success it returns a PreflightResult containing the verified config bytes.
func RunPreflight(ctx context.Context, opts *Options) (PreflightResult, error) {
	// 1. workspace is absolute
	if !filepath.IsAbs(opts.Workspace) {
		return PreflightResult{}, fmt.Errorf("preflight: workspace %q is not an absolute path", opts.Workspace)
	}

	// 2. workspace exists and is a Git working tree
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

	// 4. input_sha is a full 40-hex SHA
	if !fullSHARegexp.MatchString(opts.InputSHA) {
		return PreflightResult{}, fmt.Errorf("preflight: input_sha %q is not a full 40-hex commit SHA", opts.InputSHA)
	}

	// 5. base_sha is a full 40-hex SHA
	if !fullSHARegexp.MatchString(opts.BaseSHA) {
		return PreflightResult{}, fmt.Errorf("preflight: base_sha %q is not a full 40-hex commit SHA", opts.BaseSHA)
	}

	// 3. HEAD^{commit} exactly equals input_sha
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

	// 6. both commits exist locally
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

	// 7. base_sha is an ancestor of input_sha
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

	// 8. worktree is clean (no tracked or non-ignored untracked changes)
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

	// 9. trusted-config is absolute
	if !filepath.IsAbs(opts.TrustedConfig) {
		return PreflightResult{}, fmt.Errorf("preflight: trusted-config %q is not an absolute path", opts.TrustedConfig)
	}

	// 10 & 9 cont. — read exactly once, verify it's a regular file (not symlink)
	configInfo, err := os.Lstat(opts.TrustedConfig)
	if err != nil {
		return PreflightResult{}, fmt.Errorf("preflight: trusted-config: %w", err)
	}
	if !configInfo.Mode().IsRegular() {
		return PreflightResult{}, fmt.Errorf("preflight: trusted-config %q is not a regular file (mode: %s)", opts.TrustedConfig, configInfo.Mode())
	}
	f, err := os.Open(opts.TrustedConfig)
	if err != nil {
		return PreflightResult{}, fmt.Errorf("preflight: open trusted-config: %w", err)
	}
	configBytes, err := func() ([]byte, error) {
		defer func() { _ = f.Close() }()
		return io.ReadAll(f)
	}()
	if err != nil {
		return PreflightResult{}, fmt.Errorf("preflight: read trusted-config: %w", err)
	}

	// 11. policy_hash format
	if !policyHashRegexp.MatchString(opts.PolicyHash) {
		return PreflightResult{}, fmt.Errorf("preflight: policy_hash %q does not match sha256:<64-lowercase-hex>", opts.PolicyHash)
	}

	// 12. verify SHA-256 of config bytes matches policy_hash
	sum := sha256.Sum256(configBytes)
	computedHash := "sha256:" + hex.EncodeToString(sum[:])
	if computedHash != opts.PolicyHash {
		return PreflightResult{}, fmt.Errorf("preflight: trusted-config hash mismatch: computed %s, expected %s", computedHash, opts.PolicyHash)
	}

	// 13. evidence-dir is absolute
	if !filepath.IsAbs(opts.EvidenceDir) {
		return PreflightResult{}, fmt.Errorf("preflight: evidence-dir %q is not an absolute path", opts.EvidenceDir)
	}

	// 14. evidence-dir is outside the workspace
	wsClean := filepath.Clean(opts.Workspace) + string(filepath.Separator)
	evClean := filepath.Clean(opts.EvidenceDir)
	if strings.HasPrefix(evClean+string(filepath.Separator), wsClean) || evClean == filepath.Clean(opts.Workspace) {
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
