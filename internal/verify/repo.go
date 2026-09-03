package verify

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/douglasjarquin/made/internal/config"
	"github.com/douglasjarquin/made/internal/managed"
)

const maxTrustedConfigBytes = 1 << 20

type RepoIdentity struct {
	Root      string `json:"root"`
	OriginURL string `json:"origin_url,omitempty"`
}

type ConfigIdentity struct {
	Path string `json:"path"`
	Hash string `json:"hash"`
}

type ResolvedContext struct {
	Repository  RepoIdentity
	InputSHA    string
	BaseRef     string
	BaseSHA     string
	Config      ConfigIdentity
	ConfigBytes []byte
	Guides      []managed.GuideBinding
	Warning     string
}

func ResolveContext(ctx context.Context, workDir, baseRef string) (ResolvedContext, error) {
	if baseRef == "" {
		return ResolvedContext{}, fmt.Errorf("verify: --base-ref is required")
	}

	root, err := repoRoot(ctx, workDir)
	if err != nil {
		return ResolvedContext{}, err
	}
	canonicalRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return ResolvedContext{}, fmt.Errorf("verify: resolve canonical repository root: %w", err)
	}

	inputSHA, err := headCommit(ctx, canonicalRoot)
	if err != nil {
		return ResolvedContext{}, err
	}

	status, err := worktreeStatus(ctx, canonicalRoot)
	if err != nil {
		return ResolvedContext{}, err
	}
	if status != "" {
		return ResolvedContext{}, fmt.Errorf("verify: worktree is not clean; commit, stash, or discard changes before verifying:\n%s", status)
	}

	baseRefSHA, err := resolveCommit(ctx, canonicalRoot, baseRef)
	if err != nil {
		return ResolvedContext{}, err
	}
	baseSHA, err := mergeBase(ctx, canonicalRoot, baseRefSHA, inputSHA)
	if err != nil {
		return ResolvedContext{}, err
	}

	configPath, configBytes, warning, err := discoverTrustedConfig(canonicalRoot)
	if err != nil {
		return ResolvedContext{}, err
	}
	policyHash := hashBytes(configBytes)

	cfg, err := config.ParseBytes(configBytes)
	if err != nil {
		return ResolvedContext{}, fmt.Errorf("verify: parse trusted configuration: %w", err)
	}
	guides, err := managed.ResolveTrustedGuides(managed.TrustedGuideRoot(configPath), cfg.Review.Guides)
	if err != nil {
		return ResolvedContext{}, err
	}

	return ResolvedContext{
		Repository:  RepoIdentity{Root: canonicalRoot, OriginURL: remoteOriginURL(ctx, canonicalRoot)},
		InputSHA:    inputSHA,
		BaseRef:     baseRef,
		BaseSHA:     baseSHA,
		Config:      ConfigIdentity{Path: configPath, Hash: policyHash},
		ConfigBytes: configBytes,
		Guides:      guides,
		Warning:     warning,
	}, nil
}

// discoverTrustedConfig resolves made verify's V1-simplified notion of
// "trusted policy": the current worktree's config.Locate result, exactly as
// internal/managed already treats its --trusted-config argument as
// authoritative. There is no daemon/gate concept of a trusted base at a
// different git ref here (that stays out of scope for issue #41 - see the
// package doc and the project's PR description for the tradeoff).
func discoverTrustedConfig(root string) (path string, data []byte, warning string, err error) {
	loc, locErr := config.Locate(root)
	if locErr != nil {
		return "", nil, "", fmt.Errorf("verify: locate trusted configuration: %w", locErr)
	}
	if loc.Layout == config.LayoutAbsent {
		return absentConfigPath(root)
	}
	data, err = readBoundedRegularFile(loc.Path, maxTrustedConfigBytes)
	if err != nil {
		return "", nil, "", fmt.Errorf("verify: read trusted configuration %q: %w", loc.Path, err)
	}
	return loc.Path, data, loc.Warning, nil
}

// absentConfigPath synthesizes a minimal, valid "version: 1" configuration
// outside the repository when no first-class or legacy config file exists,
// so an absent-config repository still flows through the identical
// preflight/stage-plan path as a configured one - and, per project issue
// #39's stage planning, still truthfully fails with infrastructure_error
// rather than a meaningless pass, since no stage will resolve to "run".
func absentConfigPath(root string) (string, []byte, string, error) {
	data := []byte("version: 1\n")
	dir := filepath.Join(StateRoot(root), "config")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return "", nil, "", fmt.Errorf("verify: create synthetic empty-configuration directory: %w", err)
	}
	path := filepath.Join(dir, "empty.yaml")
	if err := writeFileAtomic(path, data, 0o600); err != nil {
		return "", nil, "", fmt.Errorf("verify: write synthetic empty configuration: %w", err)
	}
	warning := "no .made.yaml or .made/config.yaml found; verifying against an empty configuration"
	return path, data, warning, nil
}
