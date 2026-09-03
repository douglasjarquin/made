package verify

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/douglasjarquin/made/internal/config"
	"github.com/douglasjarquin/made/internal/managed"
)

type CompleteParams struct {
	WorkDir          string
	RequestPath      string
	ReviewResultPath string
}

type CompleteOutcome struct {
	Request Request
	Engine  EngineResult
	Receipt Receipt
}

func Complete(ctx context.Context, p CompleteParams) (CompleteOutcome, error) {
	if p.ReviewResultPath == "" {
		return CompleteOutcome{}, fmt.Errorf("verify: --review-result is required")
	}

	req, err := LoadRequest(p.RequestPath)
	if err != nil {
		return CompleteOutcome{}, err
	}

	root, err := repoRoot(ctx, p.WorkDir)
	if err != nil {
		return CompleteOutcome{}, err
	}
	canonicalRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return CompleteOutcome{}, fmt.Errorf("verify: resolve canonical repository root: %w", err)
	}
	if canonicalRoot != req.Repository.Root {
		return CompleteOutcome{}, fmt.Errorf("verify: repository mismatch: prepared for %q, current repository is %q", req.Repository.Root, canonicalRoot)
	}

	head, err := headCommit(ctx, canonicalRoot)
	if err != nil {
		return CompleteOutcome{}, err
	}
	if head != req.Contract.InputSHA {
		return CompleteOutcome{}, fmt.Errorf("verify: HEAD moved since prepare: prepared for input_sha %q, current HEAD is %q; run made verify prepare again", req.Contract.InputSHA, head)
	}

	status, err := worktreeStatus(ctx, canonicalRoot)
	if err != nil {
		return CompleteOutcome{}, err
	}
	if status != "" {
		return CompleteOutcome{}, fmt.Errorf("verify: worktree changed since prepare; commit, stash, or discard changes before completing:\n%s", status)
	}

	configPath, configBytes, _, err := discoverTrustedConfig(canonicalRoot)
	if err != nil {
		return CompleteOutcome{}, err
	}
	policyHash := hashBytes(configBytes)
	if configPath != req.Config.Path || policyHash != req.Config.Hash {
		return CompleteOutcome{}, fmt.Errorf("verify: trusted configuration changed since prepare: prepared %q (%s), current %q (%s); run made verify prepare again", req.Config.Path, req.Config.Hash, configPath, policyHash)
	}

	cfg, err := config.ParseBytes(configBytes)
	if err != nil {
		return CompleteOutcome{}, fmt.Errorf("verify: parse trusted configuration: %w", err)
	}
	guides, err := managed.ResolveTrustedGuides(managed.TrustedGuideRoot(configPath), cfg.Review.Guides)
	if err != nil {
		return CompleteOutcome{}, err
	}
	if !guidesEqual(guides, req.Contract.Guides) {
		return CompleteOutcome{}, fmt.Errorf("verify: configured review guides changed since prepare; run made verify prepare again")
	}

	contract := managed.BuildReviewContract(req.Contract.BaseSHA, req.Contract.InputSHA, policyHash, guides)
	contractHash, err := contract.Hash()
	if err != nil {
		return CompleteOutcome{}, fmt.Errorf("verify: hash review contract: %w", err)
	}
	if contractHash != req.ContractHash {
		return CompleteOutcome{}, fmt.Errorf("verify: review contract changed since prepare; run made verify prepare again")
	}

	stateDir := StateRoot(canonicalRoot)
	res, err := RunEngine(ctx, EngineParams{
		Workspace:    canonicalRoot,
		BaseSHA:      req.Contract.BaseSHA,
		InputSHA:     req.Contract.InputSHA,
		ConfigPath:   configPath,
		PolicyHash:   policyHash,
		ReviewSource: managed.ReviewSourceExternal,
		ReviewResult: p.ReviewResultPath,
		RunID:        req.RunID,
		MissionID:    "made-verify",
		StateDir:     stateDir,
	})
	if err != nil {
		return CompleteOutcome{}, err
	}

	executor, reviewer, requestedModel, actualModel := req.Executor, "", req.RequestedModel, ""
	if parsed, perr := managed.ParseExternalReviewResult(p.ReviewResultPath, managed.ExternalReviewIdentity{
		BaseSHA:      req.Contract.BaseSHA,
		InputSHA:     req.Contract.InputSHA,
		PolicyHash:   policyHash,
		ContractHash: contractHash,
		Guides:       guides,
	}); perr == nil {
		executor, reviewer, actualModel = parsed.Executor, parsed.Reviewer, parsed.ActualModel
		if parsed.RequestedModel != "" {
			requestedModel = parsed.RequestedModel
		}
	}

	review := reviewReceiptFromResult(res, managed.ReviewSourceExternal, executor, reviewer, requestedModel, actualModel, contractHash)
	cfgID := ConfigIdentity{Path: configPath, Hash: policyHash}
	receipt := BuildReceipt(canonicalRoot, req.Contract.BaseSHA, req.Contract.InputSHA, cfgID, review, res)

	store := ReceiptStore{Dir: ReceiptsDir(stateDir)}
	if err := store.Put(receipt); err != nil {
		return CompleteOutcome{}, fmt.Errorf("verify: write receipt: %w", err)
	}

	return CompleteOutcome{Request: req, Engine: res, Receipt: receipt}, nil
}

func guidesEqual(a, b []managed.GuideBinding) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
