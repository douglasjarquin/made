package managed_test

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/douglasjarquin/made/internal/config"
	"github.com/douglasjarquin/made/internal/managed"
	"github.com/douglasjarquin/made/internal/planner"
	"github.com/douglasjarquin/made/internal/receipt"
)

func runGitLaneReuse(t *testing.T, dir string, args ...string) string {
	t.Helper()
	full := append([]string{"-C", dir, "-c", "commit.gpgsign=false"}, args...)
	out, err := exec.Command("git", full...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return string(out)
}

// laneReuseTestRepo builds a repo with a real bare "origin" remote, so a
// receipt can be published (in test setup only - production managed/verify
// code never calls receipt.Store.Put) the same way the daemon-backed
// pipeline would.
func laneReuseTestRepo(t *testing.T) (dir, baseSHA, inputSHA string) {
	t.Helper()
	dir, baseSHA, inputSHA = makeTestRepo(t)

	remoteParent := t.TempDir()
	remote := filepath.Join(remoteParent, "remote.git")
	runGitLaneReuse(t, remoteParent, "init", "-q", "--bare", remote)
	runGitLaneReuse(t, dir, "remote", "add", "origin", remote)

	return dir, baseSHA, inputSHA
}

func laneReuseConfig() config.Config {
	return config.Config{
		Validation: config.Validation{
			Lanes: map[string]config.Lane{
				"go": {Paths: []string{"**/*.go"}, Full: []string{"echo go-full"}, RequiredBeforePush: true},
			},
		},
	}
}

// publishLaneReuseReceipt constructs the identical fingerprint
// laneExtraCommands would build for the "go" lane's Full command in dir, and
// publishes a receipt for it - simulating "a matching receipt already
// exists in local git refs", exactly as internal/managed/verify's read-only
// Store.Get would find it.
func publishLaneReuseReceipt(t *testing.T, cfg config.Config, reuse managed.LaneReuseContext, changedPaths []string, sourceRunID string) receipt.Fingerprint {
	t.Helper()
	decisions, err := planner.SelectLanes(cfg.Validation.Lanes, changedPaths)
	if err != nil {
		t.Fatalf("SelectLanes: %v", err)
	}
	configHash, err := planner.HashConfig(cfg)
	if err != nil {
		t.Fatalf("HashConfig: %v", err)
	}
	var matchedPaths []string
	for _, d := range decisions {
		if d.Name == "go" {
			matchedPaths = d.MatchedPaths
		}
	}
	fp := receipt.BuildLaneFingerprint(receipt.LaneFingerprintInputs{
		RepoIdentity: receipt.RepoIdentity(context.Background(), reuse.Workspace),
		BaseSHA:      reuse.BaseSHA,
		CandidateSHA: reuse.CandidateSHA,
		ConfigHash:   configHash,
		LaneName:     "go",
		MatchedPaths: matchedPaths,
		Command:      cfg.Validation.Lanes["go"].FullShellCommands()[0],
		MadeVersion:  managed.MadeVersion,
	})
	store := &receipt.Store{RepoPath: reuse.Workspace}
	now := time.Now().UTC()
	if _, err := store.Put(context.Background(), fp.Hash(), receipt.Receipt{
		SchemaVersion: receipt.ReceiptSchemaVersion,
		Fingerprint:   fp,
		SourceRunID:   sourceRunID,
		StartedAt:     now,
		CompletedAt:   now,
		MadeVersion:   managed.MadeVersion,
	}); err != nil {
		t.Fatalf("Put: %v", err)
	}
	return fp
}

func TestBuildStagePlan_NoMatchingReceiptRunsLaneCommandFresh(t *testing.T) {
	dir, baseSHA, inputSHA := laneReuseTestRepo(t)
	cfg := laneReuseConfig()
	reuse := managed.LaneReuseContext{Workspace: dir, BaseSHA: baseSHA, CandidateSHA: inputSHA}

	plan, err := managed.BuildStagePlan(context.Background(), cfg, []string{"hello.go"}, managed.ReviewSourceInternal, reuse)
	if err != nil {
		t.Fatalf("BuildStagePlan: %v", err)
	}
	if len(plan.Test.TestExtras) != 1 {
		t.Fatalf("expected 1 extra command on a fresh cache miss, got %+v", plan.Test)
	}
	if len(plan.Test.TestReused) != 0 {
		t.Fatalf("expected nothing reused on a fresh cache miss, got %+v", plan.Test.TestReused)
	}
}

func TestBuildStagePlan_ReusesLaneCommandFromPublishedReceipt(t *testing.T) {
	dir, baseSHA, inputSHA := laneReuseTestRepo(t)
	cfg := laneReuseConfig()
	reuse := managed.LaneReuseContext{Workspace: dir, BaseSHA: baseSHA, CandidateSHA: inputSHA}
	changedPaths := []string{"hello.go"}

	fp := publishLaneReuseReceipt(t, cfg, reuse, changedPaths, "prior-run")

	plan, err := managed.BuildStagePlan(context.Background(), cfg, changedPaths, managed.ReviewSourceInternal, reuse)
	if err != nil {
		t.Fatalf("BuildStagePlan: %v", err)
	}
	if len(plan.Test.TestExtras) != 0 {
		t.Fatalf("expected no extra commands once a receipt was published, got %+v", plan.Test.TestExtras)
	}
	if len(plan.Test.TestReused) != 1 {
		t.Fatalf("expected 1 reused command, got %+v", plan.Test.TestReused)
	}
	got := plan.Test.TestReused[0]
	if got.Name != "go" || got.SourceRunID != "prior-run" || got.FingerprintHash != fp.Hash() {
		t.Fatalf("unexpected reused entry: %+v (want fingerprint %s)", got, fp.Hash())
	}
	if plan.Test.State != managed.StagePlanRun {
		t.Fatalf("expected the test stage to still be planned to run, got %s", plan.Test.State)
	}
}

func TestBuildStagePlan_FingerprintMismatchFallsBackToFreshExecution(t *testing.T) {
	dir, baseSHA, inputSHA := laneReuseTestRepo(t)
	cfg := laneReuseConfig()
	reuse := managed.LaneReuseContext{Workspace: dir, BaseSHA: baseSHA, CandidateSHA: inputSHA}
	changedPaths := []string{"hello.go"}

	// Publish a receipt for a different candidate SHA - a real mismatch, not
	// the exact fingerprint BuildStagePlan will compute for this run.
	mismatched := reuse
	mismatched.CandidateSHA = "0000000000000000000000000000000000000f"
	publishLaneReuseReceipt(t, cfg, mismatched, changedPaths, "stale-run")

	plan, err := managed.BuildStagePlan(context.Background(), cfg, changedPaths, managed.ReviewSourceInternal, reuse)
	if err != nil {
		t.Fatalf("BuildStagePlan: %v", err)
	}
	if len(plan.Test.TestExtras) != 1 {
		t.Fatalf("expected the mismatched fingerprint to fall back to real execution, got %+v", plan.Test)
	}
	if len(plan.Test.TestReused) != 0 {
		t.Fatalf("expected nothing reused for a fingerprint mismatch, got %+v", plan.Test.TestReused)
	}
}

func TestBuildStagePlan_RepoWideNoReuseIgnoresPublishedReceipt(t *testing.T) {
	dir, baseSHA, inputSHA := laneReuseTestRepo(t)
	cfg := laneReuseConfig()
	reuse := managed.LaneReuseContext{Workspace: dir, BaseSHA: baseSHA, CandidateSHA: inputSHA}
	changedPaths := []string{"hello.go"}
	publishLaneReuseReceipt(t, cfg, reuse, changedPaths, "prior-run")

	cfg.Validation.NoReuse = true
	plan, err := managed.BuildStagePlan(context.Background(), cfg, changedPaths, managed.ReviewSourceInternal, reuse)
	if err != nil {
		t.Fatalf("BuildStagePlan: %v", err)
	}
	if len(plan.Test.TestExtras) != 1 {
		t.Fatalf("expected validation.no_reuse to force real execution, got %+v", plan.Test)
	}
	if len(plan.Test.TestReused) != 0 {
		t.Fatalf("expected nothing reused under validation.no_reuse, got %+v", plan.Test.TestReused)
	}
}

func TestBuildStagePlan_PerLaneNoReuseIgnoresPublishedReceiptForThatLaneOnly(t *testing.T) {
	dir, baseSHA, inputSHA := laneReuseTestRepo(t)
	cfg := config.Config{
		Validation: config.Validation{
			Lanes: map[string]config.Lane{
				"go":   {Paths: []string{"**/*.go"}, Full: []string{"echo go-full"}, RequiredBeforePush: true, NoReuse: true},
				"docs": {Paths: []string{"**/*.md"}, Full: []string{"echo docs-full"}, RequiredBeforePush: true},
			},
		},
	}
	reuse := managed.LaneReuseContext{Workspace: dir, BaseSHA: baseSHA, CandidateSHA: inputSHA}
	changedPaths := []string{"hello.go", "README.md"}

	decisions, err := planner.SelectLanes(cfg.Validation.Lanes, changedPaths)
	if err != nil {
		t.Fatalf("SelectLanes: %v", err)
	}
	configHash, err := planner.HashConfig(cfg)
	if err != nil {
		t.Fatalf("HashConfig: %v", err)
	}
	store := &receipt.Store{RepoPath: dir}
	now := time.Now().UTC()
	for _, d := range decisions {
		command := cfg.Validation.Lanes[d.Name].FullShellCommands()[0]
		fp := receipt.BuildLaneFingerprint(receipt.LaneFingerprintInputs{
			RepoIdentity: receipt.RepoIdentity(context.Background(), dir),
			BaseSHA:      baseSHA,
			CandidateSHA: inputSHA,
			ConfigHash:   configHash,
			LaneName:     d.Name,
			MatchedPaths: d.MatchedPaths,
			Command:      command,
			MadeVersion:  managed.MadeVersion,
		})
		if _, err := store.Put(context.Background(), fp.Hash(), receipt.Receipt{
			SchemaVersion: receipt.ReceiptSchemaVersion,
			Fingerprint:   fp,
			SourceRunID:   "prior-run",
			StartedAt:     now,
			CompletedAt:   now,
			MadeVersion:   managed.MadeVersion,
		}); err != nil {
			t.Fatalf("Put(%s): %v", d.Name, err)
		}
	}

	plan, err := managed.BuildStagePlan(context.Background(), cfg, changedPaths, managed.ReviewSourceInternal, reuse)
	if err != nil {
		t.Fatalf("BuildStagePlan: %v", err)
	}
	extraNames := map[string]bool{}
	for _, e := range plan.Test.TestExtras {
		extraNames[e.Name] = true
	}
	reusedNames := map[string]bool{}
	for _, r := range plan.Test.TestReused {
		reusedNames[r.Name] = true
	}
	if !extraNames["go"] {
		t.Fatalf("expected the go lane (no_reuse: true) to re-execute despite a published receipt, got %+v", plan.Test)
	}
	if !reusedNames["docs"] {
		t.Fatalf("expected the docs lane (no per-lane override) to be reused, got %+v", plan.Test)
	}
}
