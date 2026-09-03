package managed

import (
	"context"
	"fmt"
	"time"

	"github.com/douglasjarquin/made/internal/agent"
	"github.com/douglasjarquin/made/internal/pipeline/review"
)

// ReviewSourceInternal and ReviewSourceExternal are the only supported
// --review-source values; capabilities advertises them so a caller never
// hardcodes this list separately from what Made actually accepts.
const (
	ReviewSourceInternal = "internal"
	ReviewSourceExternal = "external"
)

// ReviewRequest is everything a ReviewSource needs to obtain (not classify)
// one Review result. It carries the exact identity a result must be bound
// to, independent of how the result is produced.
type ReviewRequest struct {
	Workspace    string
	BaseSHA      string
	InputSHA     string
	PolicyHash   string
	ContractHash string
	Timeout      time.Duration

	// Guides is the trusted base's resolved guide bindings (project issue
	// #40); nil for a project that configures none. Both InternalReviewSource
	// and ExternalReviewSource are handed the identical list.
	Guides []GuideBinding

	// Internal-only.
	AgentKind       agent.Kind
	AgentBinaryPath string
	AgentExtraEnv   []string
	RunID           string
	Evidence        *ManagedEvidenceStore

	// External-only.
	ResultPath string
}

// ReviewResult is a ReviewSource's normalized output: the findings Made's
// shared classification path consumes, regardless of source.
type ReviewResult struct {
	OK         bool
	Message    string
	Findings   []agent.Finding
	Provenance ReviewProvenance
	// GuidesConsulted is the reviewer's optional, bounded acknowledgment of
	// which configured guides it read (project issue #40); nil when no
	// guides were configured or the source never reports it.
	GuidesConsulted []GuideConsulted
}

// ReviewProvenance is optional, informational-only metadata about how a
// Review result was produced. Made never rejects a result over a
// substituted, omitted, or duplicate model value here.
type ReviewProvenance struct {
	Executor       string
	Reviewer       string
	RequestedModel string
	ActualModel    string
}

// ReviewSource obtains one Review result. It owns how the result is
// produced; classification, Decisions, evidence, and outcome are shared
// downstream logic that treats every source's output identically.
type ReviewSource interface {
	Review(ctx context.Context, req ReviewRequest) (ReviewResult, error)
}

// InternalReviewSource launches Made's own configured agent in report-only
// mode - the only source that ever spawns a Made-owned reviewer.
type InternalReviewSource struct{}

func (InternalReviewSource) Review(ctx context.Context, req ReviewRequest) (ReviewResult, error) {
	reviewOpts := review.Options{
		BaseBranch:    req.BaseSHA,
		ReportOnly:    true,
		EvidenceRunID: req.RunID,
		Guides:        toReviewGuideRefs(req.Guides),
	}
	if req.AgentBinaryPath != "" {
		reviewOpts.BinaryPath = req.AgentBinaryPath
		reviewOpts.ExtraEnv = req.AgentExtraEnv
	}

	result, err := review.Run(ctx, req.Workspace, req.AgentKind, reviewOpts)
	if err != nil {
		return ReviewResult{}, err
	}
	return ReviewResult{
		OK:       result.OK,
		Message:  result.Message,
		Findings: result.Findings,
		Provenance: ReviewProvenance{
			Executor: "made",
			Reviewer: string(req.AgentKind),
		},
	}, nil
}

// ExternalReviewSource accepts one caller-supplied review result instead of
// launching any Made-owned reviewer. It is the only source a host such as
// Cursor Cloud needs: Made never spawns a subagent on this path.
type ExternalReviewSource struct{}

func (ExternalReviewSource) Review(_ context.Context, req ReviewRequest) (ReviewResult, error) {
	if req.ResultPath == "" {
		return ReviewResult{}, fmt.Errorf("managed: review-source external requires --review-result")
	}
	parsed, err := ParseExternalReviewResult(req.ResultPath, ExternalReviewIdentity{
		BaseSHA:      req.BaseSHA,
		InputSHA:     req.InputSHA,
		PolicyHash:   req.PolicyHash,
		ContractHash: req.ContractHash,
		Guides:       req.Guides,
	})
	if err != nil {
		return ReviewResult{}, err
	}

	findings := make([]agent.Finding, 0, len(parsed.Findings))
	for _, f := range parsed.Findings {
		findings = append(findings, agent.Finding{
			Kind:        agent.FindingKind(f.Kind),
			Description: f.Description,
			Patch:       f.Patch,
			Paths:       f.Paths,
			Code:        f.Code,
			Class:       f.Class,
			Symbol:      f.Symbol,
		})
	}
	return ReviewResult{
		OK:       true,
		Message:  "external review result accepted",
		Findings: findings,
		Provenance: ReviewProvenance{
			Executor:       parsed.Executor,
			Reviewer:       parsed.Reviewer,
			RequestedModel: parsed.RequestedModel,
			ActualModel:    parsed.ActualModel,
		},
		GuidesConsulted: parsed.GuidesConsulted,
	}, nil
}

func toReviewGuideRefs(guides []GuideBinding) []agent.ReviewGuideRef {
	if len(guides) == 0 {
		return nil
	}
	refs := make([]agent.ReviewGuideRef, 0, len(guides))
	for _, g := range guides {
		refs = append(refs, agent.ReviewGuideRef{Path: g.Path, ContentHash: g.ContentHash, Bytes: g.Bytes})
	}
	return refs
}

func resolveReviewSource(name string) (ReviewSource, error) {
	switch name {
	case ReviewSourceInternal:
		return InternalReviewSource{}, nil
	case ReviewSourceExternal:
		return ExternalReviewSource{}, nil
	default:
		return nil, fmt.Errorf("managed: unsupported review source %q; supported: %q, %q", name, ReviewSourceInternal, ReviewSourceExternal)
	}
}
