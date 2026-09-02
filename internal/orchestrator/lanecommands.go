package orchestrator

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os/exec"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/douglasjarquin/made/internal/api"
	execpkg "github.com/douglasjarquin/made/internal/exec"
	"github.com/douglasjarquin/made/internal/pipeline/test"
	"github.com/douglasjarquin/made/internal/planner"
	"github.com/douglasjarquin/made/internal/receipt"
)

// laneCommandFingerprint pairs one Full command with the fingerprint it was
// computed under, so publishLaneReceipts can publish a receipt for exactly
// the commands that actually ran (and only after Test reports success -
// laneTestPlan itself never claims anything succeeded).
type laneCommandFingerprint struct {
	Name        string
	Fingerprint receipt.Fingerprint
}

// laneTestPlan is laneFullCommandsForTest's result: Extras are the lane Full
// commands the Test stage must actually run; Reused names lanes a valid
// receipt already covers, for visibility only - nothing downstream treats
// Reused as having "passed" beyond simply not needing to run again.
type laneTestPlan struct {
	Extras       []test.ExtraCommand
	Reused       []string
	fingerprints []laneCommandFingerprint
}

// laneFullCommandsForTest resolves, for the candidate currently in the gate
// worktree, which validation.lanes (config #33 Phase 2) are selected, and
// which of their Full commands must actually run versus can be satisfied by
// an existing receipt (project issue #33 Phase 3). With no lanes configured
// it returns a zero-value laneTestPlan: zero behavior change from before
// lanes existed.
func (c *chain) laneFullCommandsForTest() (laneTestPlan, error) {
	lanes := c.rc.Config.Validation.Lanes
	if len(lanes) == 0 {
		return laneTestPlan{}, nil
	}

	changedPaths, err := planner.ChangedPaths(c.ctx, c.rc.Worktree.Path, c.defaultBranch, "HEAD")
	if err != nil {
		return laneTestPlan{}, fmt.Errorf("orchestrator: compute changed paths for validation lanes: %w", err)
	}
	decisions, err := planner.SelectLanes(lanes, changedPaths)
	if err != nil {
		return laneTestPlan{}, fmt.Errorf("orchestrator: select validation lanes: %w", err)
	}

	baseSHA, err := resolveWorktreeRef(c.ctx, c.rc.Worktree.Path, c.defaultBranch)
	if err != nil {
		return laneTestPlan{}, fmt.Errorf("orchestrator: resolve base SHA for validation lanes: %w", err)
	}
	candidateSHA, err := resolveWorktreeRef(c.ctx, c.rc.Worktree.Path, "HEAD")
	if err != nil {
		return laneTestPlan{}, fmt.Errorf("orchestrator: resolve candidate SHA for validation lanes: %w", err)
	}
	configHash, err := planner.HashConfig(c.rc.Config)
	if err != nil {
		return laneTestPlan{}, fmt.Errorf("orchestrator: hash effective config for validation lanes: %w", err)
	}
	repoIdentity := originRemoteURL(c.ctx, c.rc.Worktree.Path)
	receiptStore := &receipt.Store{RepoPath: c.rc.Worktree.Path}
	noReuse := c.rc.Config.Validation.NoReuse

	var plan laneTestPlan
	for _, decision := range decisions {
		if decision.Action != "run" {
			continue
		}
		lane, ok := lanes[decision.Name]
		if !ok {
			continue
		}
		commands := lane.FullShellCommands()
		for i, cmd := range commands {
			name := decision.Name
			if len(commands) > 1 {
				name = fmt.Sprintf("%s-%d", decision.Name, i+1)
			}
			fp := buildLaneFingerprint(laneFingerprintInputs{
				repoIdentity: repoIdentity,
				baseSHA:      baseSHA,
				candidateSHA: candidateSHA,
				configHash:   configHash,
				laneName:     decision.Name,
				matchedPaths: decision.MatchedPaths,
				command:      cmd,
			})
			if !noReuse {
				if _, found, _ := receiptStore.Get(c.ctx, fp.Hash()); found {
					plan.Reused = append(plan.Reused, name)
					continue
				}
			}
			plan.Extras = append(plan.Extras, test.ExtraCommand{Name: name, Args: cmd})
			plan.fingerprints = append(plan.fingerprints, laneCommandFingerprint{Name: name, Fingerprint: fp})
		}
	}
	return plan, nil
}

// publishLaneReceipts records a receipt for every command in plan.Extras,
// which the caller must only invoke after confirming the Test stage
// reported overall success - test.Run stops at the first failing command,
// so success there means every one of plan.Extras actually passed. A
// publish failure is swallowed: reuse is an optimization, and a run that
// otherwise succeeded must never fail because recording that fact didn't
// work.
func (c *chain) publishLaneReceipts(plan laneTestPlan) {
	if len(plan.fingerprints) == 0 {
		return
	}
	store := &receipt.Store{RepoPath: c.rc.Worktree.Path}
	now := time.Now().UTC()
	for _, entry := range plan.fingerprints {
		r := receipt.Receipt{
			SchemaVersion: receipt.ReceiptSchemaVersion,
			Fingerprint:   entry.Fingerprint,
			SourceRunID:   c.runID,
			StartedAt:     now,
			CompletedAt:   now,
			MadeVersion:   madeVersion,
		}
		_, _ = store.Put(c.ctx, entry.Fingerprint.Hash(), r)
	}
}

// madeVersion is a placeholder made version identifier until a real
// build-time version string exists; it only affects fingerprints, never
// program behavior.
const madeVersion = "dev"

type laneFingerprintInputs struct {
	repoIdentity string
	baseSHA      string
	candidateSHA string
	configHash   string
	laneName     string
	matchedPaths []string
	command      []string
}

func buildLaneFingerprint(in laneFingerprintInputs) receipt.Fingerprint {
	return receipt.Fingerprint{
		SchemaVersion:    receipt.FingerprintSchemaVersion,
		Lane:             in.laneName,
		ValidationLevel:  "full",
		RepoIdentity:     in.repoIdentity,
		BaseSHA:          in.baseSHA,
		CandidateSHA:     in.candidateSHA,
		ConfigHash:       in.configHash,
		Command:          in.command,
		WorkingDirectory: ".",
		InputSetHash:     hashPathSet(in.matchedPaths),
		ToolchainHash:    toolchainFingerprint(),
		OS:               runtime.GOOS,
		Arch:             runtime.GOARCH,
		MadeVersion:      madeVersion,
		ProtocolVersion:  api.Version,
	}
}

func hashPathSet(paths []string) string {
	sorted := append([]string(nil), paths...)
	sort.Strings(sorted)
	data, err := json.Marshal(sorted)
	if err != nil {
		// paths is always a []string; Marshal cannot fail for that shape.
		panic("orchestrator: path set is not JSON-marshalable: " + err.Error())
	}
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// toolchainFingerprint is a coarse, best-effort proxy for "the Go toolchain
// that would build/test this candidate": the exact output of `go version`.
// It says nothing about installed system libraries, other language
// toolchains, or transitive tool versions - refining it is left to a later
// iteration once basic reuse is proven safe. Any failure to even run `go
// version` yields a fixed sentinel, so a fingerprint is still well-defined
// (and simply never matches a receipt from an environment where it did
// succeed).
func toolchainFingerprint() string {
	out, err := exec.Command("go", "version").Output()
	if err != nil {
		return "unknown"
	}
	sum := sha256.Sum256(out)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func resolveWorktreeRef(ctx context.Context, repoPath, ref string) (string, error) {
	res, err := execpkg.Run(ctx, execpkg.Command{
		Name: "git",
		Args: []string{"rev-parse", "--verify", ref + "^{commit}"},
		Dir:  repoPath,
	})
	if err != nil {
		return "", err
	}
	if res.ExitCode != 0 {
		return "", fmt.Errorf("git rev-parse %s failed: %s", ref, strings.TrimSpace(string(res.Stderr)))
	}
	return strings.TrimSpace(string(res.Stdout)), nil
}
