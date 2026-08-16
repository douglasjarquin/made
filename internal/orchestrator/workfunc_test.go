package orchestrator

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/douglasjarquin/made/internal/agent"
	"github.com/douglasjarquin/made/internal/agent/agenttest"
	"github.com/douglasjarquin/made/internal/config"
	"github.com/douglasjarquin/made/internal/daemon"
	"github.com/douglasjarquin/made/internal/evidence"
	"github.com/douglasjarquin/made/internal/gitgate"
	"github.com/douglasjarquin/made/internal/github"
	"github.com/douglasjarquin/made/internal/github/githubtest"
	"github.com/douglasjarquin/made/internal/pipeline/review"
)

type wfFixture struct {
	dir           string
	barePath      string
	realRemote    string
	worktreesDir  string
	src           string
	defaultBranch string
}

func TestChain_RefusesDeliveryWhenRequiredStageDisabled(t *testing.T) {
	disabled := false
	c := &chain{rc: &RunContext{Config: config.Config{
		Stages: map[string]config.Stage{stageNameReview: {Enabled: &disabled}},
	}}}

	err := c.requireDeliveryStages()
	if err == nil {
		t.Fatal("requireDeliveryStages allowed delivery with a disabled required review stage")
	}
	if !strings.Contains(err.Error(), `required stage "review" is disabled`) {
		t.Fatalf("requireDeliveryStages error = %q, want disabled review stage", err)
	}
}

func newWFFixture(t *testing.T) *wfFixture {
	t.Helper()
	dir := t.TempDir()

	barePath := filepath.Join(dir, "gate.git")
	if err := gitgate.InitBare(barePath); err != nil {
		t.Fatalf("InitBare: %v", err)
	}

	runGit(t, dir, "init", "--bare", "-q", "real-remote.git")
	realRemote := filepath.Join(dir, "real-remote.git")
	runGit(t, barePath, "remote", "add", "origin", realRemote)

	src := filepath.Join(dir, "src")
	initSourceRepo(t, src)
	pushBranch(t, src, barePath, "main")

	return &wfFixture{
		dir:           dir,
		barePath:      barePath,
		realRemote:    realRemote,
		worktreesDir:  filepath.Join(dir, "worktrees"),
		src:           src,
		defaultBranch: "main",
	}
}

func (f *wfFixture) pushFeature(t *testing.T, branch, intentSummary, fileName, content string) string {
	t.Helper()
	runGit(t, f.src, "checkout", "-b", branch)
	writeFile(t, f.src, fileName, content)
	commitWithIntent(t, f.src, "feature work", intentSummary)
	return pushBranch(t, f.src, f.barePath, branch)
}

func (f *wfFixture) worktree(t *testing.T, sha string) *gitgate.Worktree {
	t.Helper()
	wt, err := gitgate.AddWorktree(f.barePath, f.worktreesDir, sha)
	if err != nil {
		t.Fatalf("AddWorktree: %v", err)
	}
	return wt
}

func (f *wfFixture) branchOnRealRemote(t *testing.T, branch string) bool {
	t.Helper()
	out, err := exec.Command("git", "ls-remote", f.realRemote, "refs/heads/"+branch).CombinedOutput()
	if err != nil {
		t.Fatalf("ls-remote: %v: %s", err, out)
	}
	return strings.TrimSpace(string(out)) != ""
}

func commitWithIntent(t *testing.T, dir, title, intentSummary string) {
	t.Helper()
	runGit(t, dir, "add", "-A")
	cmd := exec.Command("git", "commit", "-q", "-m", title, "-m", "Intent: "+intentSummary)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=orchestrator-test",
		"GIT_AUTHOR_EMAIL=orchestrator-test@example.com",
		"GIT_COMMITTER_NAME=orchestrator-test",
		"GIT_COMMITTER_EMAIL=orchestrator-test@example.com",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git commit (with intent) in %s failed: %v: %s", dir, err, out)
	}
}

func writeScenario(t *testing.T, findings agent.Findings) string {
	t.Helper()
	data, err := json.Marshal(findings)
	if err != nil {
		t.Fatalf("marshal scenario: %v", err)
	}
	path := filepath.Join(t.TempDir(), "scenario.json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write scenario: %v", err)
	}
	return path
}

func newRunContext(wt *gitgate.Worktree, cfg config.Config, ghBin, ghLog string) *RunContext {
	store := evidence.NewStore(wt.Path, evidence.Config{StoreInRepo: true, Dir: ".made/evidence"})
	env := os.Environ()
	if ghLog != "" {
		env = append(env, "FAKE_GH_LOG_FILE="+ghLog)
	}
	return &RunContext{
		Config:   cfg,
		Worktree: wt,
		Evidence: store,
		GitHub:   &github.Client{Binary: ghBin, Dir: wt.Path, ExtraEnv: env},
	}
}

func waitForRunEnded(t *testing.T, rm *daemon.RunManager, id string, timeout time.Duration) daemon.RunSnapshot {
	t.Helper()
	deadline := time.After(timeout)
	for {
		snap, ok := rm.Snapshot(id)
		if ok && !snap.EndedAt.IsZero() {
			return snap
		}
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for run %q to end (ok=%v last=%+v)", id, ok, snap)
		case <-time.After(5 * time.Millisecond):
		}
	}
}

func waitForPendingFindings(t *testing.T, rm *daemon.RunManager, id string, timeout time.Duration) daemon.RunSnapshot {
	t.Helper()
	deadline := time.After(timeout)
	for {
		snap, ok := rm.Snapshot(id)
		if ok && len(snap.PendingFindings) > 0 {
			return snap
		}
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for run %q to have pending findings (ok=%v last=%+v)", id, ok, snap)
		case <-time.After(5 * time.Millisecond):
		}
	}
}

var wantAllStages = []string{
	stageNameIntent, stageNameRebase, stageNameReview, stageNameTest,
	stageNameDocument, stageNameLint, stageNamePush, stageNamePR, stageNameCI,
}

func assertAllStagesPassed(t *testing.T, stages []daemon.StageResult) {
	t.Helper()
	if len(stages) != len(wantAllStages) {
		t.Fatalf("expected %d recorded stages, got %+v", len(wantAllStages), stages)
	}
	for i, name := range wantAllStages {
		if stages[i].Name != name || stages[i].Result != stageResultPass {
			t.Fatalf("stage %d: got %+v, want name=%s result=%s", i, stages[i], name, stageResultPass)
		}
	}
}

func cleanReviewOptions(t *testing.T) review.Options {
	t.Helper()
	bin := agenttest.Build(t)
	scenarioPath := writeScenario(t, agent.Findings{})
	return review.Options{
		BinaryPath: bin,
		ExtraEnv:   []string{"FAKE_AGENT_SCENARIO=" + scenarioPath},
	}
}

func submitWorkFunc(t *testing.T, rm *daemon.RunManager, runID, repo, branch string, wf WorkFunc, rc *RunContext) {
	t.Helper()
	if _, err := rm.Submit(runID, repo, branch, func(ctx context.Context, _ func(daemon.Event)) error {
		return wf(ctx, rc)
	}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
}

func TestNewWorkFunc_FullPassEndsRunningWithAwaitingMergeMessage(t *testing.T) {
	f := newWFFixture(t)
	branch := "feature-full-pass"
	sha := f.pushFeature(t, branch, "add greeting file", "greeting.txt", "hello\n")
	wt := f.worktree(t, sha)
	defer func() { _ = wt.Remove() }()

	ghBin := githubtest.Build(t)
	cfg := config.Config{
		Agent:    string(agent.KindClaude),
		Commands: config.Commands{Test: "true", Lint: "true"},
		CI:       config.CI{RerunBudget: 1},
	}
	rc := newRunContext(wt, cfg, ghBin, "")

	rm := daemon.NewRunManager()
	reviewDecisions := daemon.NewReviewDecisions()
	runID := rm.NewRunID()

	wf := NewWorkFunc(rm, reviewDecisions, nil, runID, f.defaultBranch, branch, Options{
		ReviewOptions: cleanReviewOptions(t),
	})
	submitWorkFunc(t, rm, runID, "repo-full-pass", branch, wf, rc)

	snap := waitForRunEnded(t, rm, runID, 30*time.Second)
	if snap.Status != daemon.RunAwaitingMerge {
		t.Fatalf("expected final status RunAwaitingMerge, got %v (err=%v)", snap.Status, snap.Err)
	}
	if !strings.Contains(snap.Message, "awaiting merge") {
		t.Fatalf("expected final message to mention awaiting merge, got %q", snap.Message)
	}
	assertAllStagesPassed(t, snap.Stages)

	if !f.branchOnRealRemote(t, branch) {
		t.Fatalf("expected %s to have been pushed to the real remote", branch)
	}
}

func TestNewWorkFunc_FullPassPRTitleMatchesPushedCommitSubject(t *testing.T) {
	f := newWFFixture(t)
	branch := "feature-pr-title"
	sha := f.pushFeature(t, branch, "add greeting file", "greeting.txt", "hello\n")
	wt := f.worktree(t, sha)
	defer func() { _ = wt.Remove() }()

	ghBin := githubtest.Build(t)
	ghLog := filepath.Join(t.TempDir(), "gh-invocations.log")
	cfg := config.Config{
		Agent:    string(agent.KindClaude),
		Commands: config.Commands{Test: "true", Lint: "true"},
		CI:       config.CI{RerunBudget: 1},
	}
	rc := newRunContext(wt, cfg, ghBin, ghLog)

	rm := daemon.NewRunManager()
	reviewDecisions := daemon.NewReviewDecisions()
	runID := rm.NewRunID()

	wf := NewWorkFunc(rm, reviewDecisions, nil, runID, f.defaultBranch, branch, Options{
		ReviewOptions: cleanReviewOptions(t),
	})
	submitWorkFunc(t, rm, runID, "repo-pr-title", branch, wf, rc)

	snap := waitForRunEnded(t, rm, runID, 30*time.Second)
	if snap.Status != daemon.RunAwaitingMerge {
		t.Fatalf("expected final status RunAwaitingMerge, got %v (err=%v)", snap.Status, snap.Err)
	}
	assertAllStagesPassed(t, snap.Stages)

	wantSubject, err := derivePRTitle(wt.Path)
	if err != nil {
		t.Fatalf("derivePRTitle (for expectation): %v", err)
	}
	if wantSubject != "feature work" {
		t.Fatalf("test fixture assumption broken: expected pushFeature's commit subject to be %q, got %q", "feature work", wantSubject)
	}

	data, err := os.ReadFile(ghLog)
	if err != nil {
		t.Fatalf("read gh invocation log: %v", err)
	}
	log := string(data)
	if !strings.Contains(log, "pr create") {
		t.Fatalf("expected a pr create invocation in gh log, got:\n%s", log)
	}
	wantArg := "--title " + wantSubject
	if !strings.Contains(log, wantArg) {
		t.Fatalf("expected gh invocation log to contain %q (PR title matching the pushed commit's subject), got:\n%s", wantArg, log)
	}
}

func TestNewWorkFunc_TestFailureHaltsBeforeLaterStages(t *testing.T) {
	f := newWFFixture(t)
	branch := "feature-test-fail"
	sha := f.pushFeature(t, branch, "add file", "file.txt", "content\n")
	wt := f.worktree(t, sha)
	defer func() { _ = wt.Remove() }()

	ghBin := githubtest.Build(t)
	lintMarker := filepath.Join(t.TempDir(), "lint-ran.marker")
	ghLog := filepath.Join(t.TempDir(), "gh-invocations.log")

	cfg := config.Config{
		Agent: string(agent.KindClaude),
		Commands: config.Commands{
			Test: "exit 1",
			Lint: "touch " + lintMarker,
		},
		CI: config.CI{RerunBudget: 1},
	}
	rc := newRunContext(wt, cfg, ghBin, ghLog)

	rm := daemon.NewRunManager()
	reviewDecisions := daemon.NewReviewDecisions()
	runID := rm.NewRunID()

	wf := NewWorkFunc(rm, reviewDecisions, nil, runID, f.defaultBranch, branch, Options{
		ReviewOptions: cleanReviewOptions(t),
	})
	submitWorkFunc(t, rm, runID, "repo-test-fail", branch, wf, rc)

	snap := waitForRunEnded(t, rm, runID, 30*time.Second)
	if snap.Status != daemon.RunFailed {
		t.Fatalf("expected RunFailed, got %v (message=%q)", snap.Status, snap.Message)
	}

	if snap.Err == nil {
		t.Fatalf("expected a non-nil error for a pre-push stage failure")
	}
	if got, want := snap.Err.Error(), `orchestrator: stage "test" failed:`; !strings.Contains(got, want) {
		t.Fatalf("expected the original generic failure message %q, got %q", want, got)
	}
	if strings.Contains(snap.Err.Error(), "push succeeded") {
		t.Fatalf("did not expect push-succeeded wording for a failure that happened before push ran, got %q", snap.Err.Error())
	}

	wantStages := []string{stageNameIntent, stageNameRebase, stageNameReview, stageNameTest}
	if len(snap.Stages) != len(wantStages) {
		t.Fatalf("expected exactly %d recorded stages (halted at test), got %+v", len(wantStages), snap.Stages)
	}
	last := snap.Stages[len(snap.Stages)-1]
	if last.Name != stageNameTest || last.Result != stageResultFail {
		t.Fatalf("expected final recorded stage to be test/fail, got %+v", last)
	}

	if _, err := os.Stat(lintMarker); !os.IsNotExist(err) {
		t.Fatalf("expected lint to never run (marker file must not exist), stat err=%v", err)
	}
	if _, err := os.Stat(ghLog); !os.IsNotExist(err) {
		t.Fatalf("expected gh to never be invoked (log file must not exist), stat err=%v", err)
	}
	if f.branchOnRealRemote(t, branch) {
		t.Fatalf("expected %s to never reach the real remote", branch)
	}
}

func TestNewWorkFunc_DocumentFindingParksThenRejectedFailsRun(t *testing.T) {
	f := newWFFixture(t)
	branch := "feature-doc-reject"
	sha := f.pushFeature(t, branch, "add code without docs", "pkg/code.go", "package pkg\n")
	wt := f.worktree(t, sha)
	defer func() { _ = wt.Remove() }()

	ghBin := githubtest.Build(t)
	cfg := config.Config{
		Agent:    string(agent.KindClaude),
		Commands: config.Commands{Test: "true", Lint: "true"},
		CI:       config.CI{RerunBudget: 1},
		Document: config.Document{Rules: []config.DocumentRule{
			{PathPattern: "pkg/*.go", RequiredDocPattern: "docs/*.md"},
		}},
	}
	rc := newRunContext(wt, cfg, ghBin, "")

	rm := daemon.NewRunManager()
	reviewDecisions := daemon.NewReviewDecisions()
	runID := rm.NewRunID()

	wf := NewWorkFunc(rm, reviewDecisions, nil, runID, f.defaultBranch, branch, Options{
		ReviewOptions: cleanReviewOptions(t),
	})
	submitWorkFunc(t, rm, runID, "repo-doc-reject", branch, wf, rc)

	parked := waitForPendingFindings(t, rm, runID, 30*time.Second)
	if parked.Status != daemon.RunAwaitingReview {
		t.Fatalf("expected parked run to stay RunAwaitingReview, got %v", parked.Status)
	}
	if len(parked.PendingFindings) != 1 || parked.PendingFindings[0].Stage != stageNameDocument {
		t.Fatalf("expected one pending finding on stage %q, got %+v", stageNameDocument, parked.PendingFindings)
	}

	reviewDecisions.Set(runID, stageNameDocument, daemon.ReviewRejected)

	snap := waitForRunEnded(t, rm, runID, 30*time.Second)
	if snap.Status != daemon.RunFailed {
		t.Fatalf("expected RunFailed after rejection, got %v", snap.Status)
	}
	if len(snap.PendingFindings) != 0 {
		t.Fatalf("expected pending findings cleared after decision, got %+v", snap.PendingFindings)
	}
}

func TestNewWorkFunc_DocumentFindingParksThenApprovedResumesToCompletion(t *testing.T) {
	f := newWFFixture(t)
	branch := "feature-doc-approve"
	sha := f.pushFeature(t, branch, "add code without docs", "pkg/code.go", "package pkg\n")
	wt := f.worktree(t, sha)
	defer func() { _ = wt.Remove() }()

	ghBin := githubtest.Build(t)
	cfg := config.Config{
		Agent:    string(agent.KindClaude),
		Commands: config.Commands{Test: "true", Lint: "true"},
		CI:       config.CI{RerunBudget: 1},
		Document: config.Document{Rules: []config.DocumentRule{
			{PathPattern: "pkg/*.go", RequiredDocPattern: "docs/*.md"},
		}},
	}
	rc := newRunContext(wt, cfg, ghBin, "")

	rm := daemon.NewRunManager()
	reviewDecisions := daemon.NewReviewDecisions()
	runID := rm.NewRunID()

	wf := NewWorkFunc(rm, reviewDecisions, nil, runID, f.defaultBranch, branch, Options{
		ReviewOptions: cleanReviewOptions(t),
	})
	submitWorkFunc(t, rm, runID, "repo-doc-approve", branch, wf, rc)

	parked := waitForPendingFindings(t, rm, runID, 30*time.Second)
	if parked.Status != daemon.RunAwaitingReview {
		t.Fatalf("expected parked run to stay RunAwaitingReview, got %v", parked.Status)
	}

	reviewDecisions.Set(runID, stageNameDocument, daemon.ReviewApproved)

	snap := waitForRunEnded(t, rm, runID, 30*time.Second)
	if snap.Status != daemon.RunAwaitingMerge {
		t.Fatalf("expected final status RunAwaitingMerge after resume, got %v (err=%v)", snap.Status, snap.Err)
	}
	if !strings.Contains(snap.Message, "awaiting merge") {
		t.Fatalf("expected final message to mention awaiting merge, got %q", snap.Message)
	}
	assertAllStagesPassed(t, snap.Stages)
	if len(snap.PendingFindings) != 0 {
		t.Fatalf("expected pending findings cleared, got %+v", snap.PendingFindings)
	}
	if !f.branchOnRealRemote(t, branch) {
		t.Fatalf("expected %s to have been pushed to the real remote", branch)
	}
}

func newRunContextWithFailingPR(wt *gitgate.Worktree, cfg config.Config, ghBin, stderrMsg string) *RunContext {
	store := evidence.NewStore(wt.Path, evidence.Config{StoreInRepo: true, Dir: ".made/evidence"})
	env := append(os.Environ(), "FAKE_GH_EXIT_CODE=1", "FAKE_GH_STDERR="+stderrMsg)
	return &RunContext{
		Config:   cfg,
		Worktree: wt,
		Evidence: store,
		GitHub:   &github.Client{Binary: ghBin, Dir: wt.Path, ExtraEnv: env},
	}
}

func TestNewWorkFunc_PushSucceedsThenPRFailsMessageNamesPushedBranch(t *testing.T) {
	f := newWFFixture(t)
	branch := "feature-push-then-pr-fail"
	sha := f.pushFeature(t, branch, "add file", "file.txt", "content\n")
	wt := f.worktree(t, sha)
	defer func() { _ = wt.Remove() }()

	ghBin := githubtest.Build(t)
	cfg := config.Config{
		Agent:    string(agent.KindClaude),
		Commands: config.Commands{Test: "true", Lint: "true"},
		CI:       config.CI{RerunBudget: 1},
	}
	rc := newRunContextWithFailingPR(wt, cfg, ghBin, "insufficient permissions to open pull request")

	rm := daemon.NewRunManager()
	reviewDecisions := daemon.NewReviewDecisions()
	runID := rm.NewRunID()

	wf := NewWorkFunc(rm, reviewDecisions, nil, runID, f.defaultBranch, branch, Options{
		ReviewOptions: cleanReviewOptions(t),
	})
	submitWorkFunc(t, rm, runID, "repo-push-then-pr-fail", branch, wf, rc)

	snap := waitForRunEnded(t, rm, runID, 30*time.Second)
	if snap.Status != daemon.RunFailed {
		t.Fatalf("expected RunFailed, got %v (err=%v)", snap.Status, snap.Err)
	}

	if !f.branchOnRealRemote(t, branch) {
		t.Fatalf("expected %s to have already been pushed to the real remote before PR creation failed", branch)
	}

	if snap.Err == nil {
		t.Fatalf("expected a non-nil error")
	}
	errMsg := snap.Err.Error()
	for _, want := range []string{
		"push succeeded",
		branch,
		"origin",
		"PR creation failed",
		"insufficient permissions to open pull request",
		"the branch is live on the real remote, no automatic action taken",
	} {
		if !strings.Contains(errMsg, want) {
			t.Fatalf("expected failure message to contain %q, got %q", want, errMsg)
		}
	}

	wantStages := []string{
		stageNameIntent, stageNameRebase, stageNameReview, stageNameTest,
		stageNameDocument, stageNameLint, stageNamePush, stageNamePR,
	}
	if len(snap.Stages) != len(wantStages) {
		t.Fatalf("expected exactly %d recorded stages (halted at pr), got %+v", len(wantStages), snap.Stages)
	}
	last := snap.Stages[len(snap.Stages)-1]
	if last.Name != stageNamePR || last.Result != stageResultFail {
		t.Fatalf("expected final recorded stage to be pr/fail, got %+v", last)
	}
}
