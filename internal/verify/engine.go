package verify

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/douglasjarquin/made/internal/config"
	"github.com/douglasjarquin/made/internal/managed"
	"github.com/douglasjarquin/made/internal/planner"
)

type EngineParams struct {
	Workspace    string
	BaseSHA      string
	InputSHA     string
	ConfigPath   string
	PolicyHash   string
	ReviewSource string
	ReviewResult string
	RunID        string
	MissionID    string
	StateDir     string
}

type EngineResult struct {
	InvocationID     string
	Outcome          managed.Outcome
	Message          string
	StoppedAt        string
	StageResults     []managed.StageResult
	Findings         []managed.FindingReportedPayload
	EvidenceRefs     []string
	DecisionsApplied []string
	EvidenceDir      string
	Guides           []managed.GuideBinding
}

func RunEngine(ctx context.Context, p EngineParams) (EngineResult, error) {
	invocationID, err := newInvocationID()
	if err != nil {
		return EngineResult{}, fmt.Errorf("verify: generate invocation id: %w", err)
	}

	evidenceDir := EvidenceRoot(p.StateDir)
	opts := &managed.Options{
		RunID:         p.RunID,
		MissionID:     p.MissionID,
		Workspace:     p.Workspace,
		BaseSHA:       p.BaseSHA,
		InputSHA:      p.InputSHA,
		TrustedConfig: p.ConfigPath,
		PolicyHash:    p.PolicyHash,
		EvidenceDir:   evidenceDir,
		ReviewSource:  p.ReviewSource,
		ReviewResult:  p.ReviewResult,
		InvocationID:  invocationID,
	}
	if err := managed.ValidateOptions(opts); err != nil {
		return EngineResult{}, fmt.Errorf("verify: %w", err)
	}

	fail := func(stage, message string) (EngineResult, error) {
		return EngineResult{
			InvocationID: invocationID,
			Outcome:      managed.OutcomeInfrastructureError,
			Message:      message,
			StoppedAt:    stage,
			EvidenceDir:  evidenceDir,
		}, nil
	}

	preflight, err := managed.RunPreflight(ctx, opts)
	if err != nil {
		if ctx.Err() != nil {
			return EngineResult{
				InvocationID: invocationID,
				Outcome:      managed.OutcomeCanceled,
				Message:      "canceled during preflight: " + ctx.Err().Error(),
				StoppedAt:    "canceled",
				EvidenceDir:  evidenceDir,
			}, nil
		}
		return fail("preflight", "preflight: "+err.Error())
	}

	cfg, err := config.ParseBytes(preflight.ConfigBytes)
	if err != nil {
		return fail("preflight", "parse trusted config: "+err.Error())
	}

	guides, err := managed.ResolveTrustedGuides(managed.TrustedGuideRoot(opts.TrustedConfig), cfg.Review.Guides)
	if err != nil {
		return fail("preflight", "resolve review guides: "+err.Error())
	}

	decisions, err := managed.LoadDecisions("", opts)
	if err != nil {
		return EngineResult{}, fmt.Errorf("verify: load decisions: %w", err)
	}

	ev := managed.NewManagedEvidenceStore(opts.EvidenceDir, opts.RunID, invocationID)
	if err := os.MkdirAll(ev.InvocationDir(), 0o750); err != nil {
		return fail("preflight", "create evidence directory: "+err.Error())
	}

	changedPaths, err := planner.ChangedPaths(ctx, opts.Workspace, opts.BaseSHA, opts.InputSHA)
	if err != nil {
		return fail("preflight", "compute changed paths: "+err.Error())
	}
	plan, err := managed.BuildStagePlan(cfg, changedPaths, opts.ReviewSource)
	if err != nil {
		return fail("preflight", "build stage plan: "+err.Error())
	}

	ew := managed.NewEventWriter(io.Discard, opts)
	runner := managed.NewRunner(opts, cfg, ew, ev, decisions, plan, guides)
	outcome, msg, stoppedAt := runner.Run(ctx)
	if ctx.Err() != nil {
		outcome = managed.OutcomeCanceled
		msg = "canceled: " + ctx.Err().Error()
		stoppedAt = "canceled"
	}

	manifest := &managed.TerminalManifest{
		SchemaVersion:    managed.TerminalManifestSchemaVersion,
		RunID:            opts.RunID,
		MissionID:        opts.MissionID,
		InvocationID:     invocationID,
		BaseSHA:          opts.BaseSHA,
		InputSHA:         opts.InputSHA,
		PolicyHash:       opts.PolicyHash,
		StageResults:     runner.StageResults(),
		Findings:         runner.AllFindings(),
		DecisionsApplied: runner.DecisionsApplied(),
		Outcome:          outcome,
		EvidenceRefs:     runner.EvidenceRefs(),
		MadeVersion:      managed.MadeVersion,
	}
	if writeErr := ev.WriteTerminal(manifest); writeErr != nil {
		outcome = managed.OutcomeInfrastructureError
		msg = "terminal evidence write failed: " + writeErr.Error()
	}

	return EngineResult{
		InvocationID:     invocationID,
		Outcome:          outcome,
		Message:          msg,
		StoppedAt:        stoppedAt,
		StageResults:     runner.StageResults(),
		Findings:         runner.AllFindings(),
		EvidenceRefs:     runner.EvidenceRefs(),
		DecisionsApplied: runner.DecisionsApplied(),
		EvidenceDir:      filepath.Join(evidenceDir),
		Guides:           guides,
	}, nil
}

func newInvocationID() (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}
