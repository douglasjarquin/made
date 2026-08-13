# made: wire the pipeline orchestrator (close the F1/F3 gap)

## TL;DR
> **Summary**: plans/made-rewrite.md built all 9 pipeline stages, the socket API, and the run manager as separately-tested packages, but no task ever wired them into an actual git-push-triggered run - F1/F3 of that plan's Final Verification Wave found zero production callers of `RunManager.Submit` and no `made gate` CLI at all. This plan closes that gap: real socket-authenticated push admission (dropping the placeholder shared-secret token), a `made gate init` CLI, and an `internal/orchestrator` package that chains all 9 stages into one real run, including the ask-user park/resume semantics and production-hardening fixes Metis found missing.
> **Deliverables**: gitgate hook rewritten for socket-only auth; `made gate init/admit-push/notify-push` CLI; `gate.admitPush`/`gate.notifyPush` socket RPCs; `internal/orchestrator` package; `daemon.RunSnapshot` carrying real per-run stage/finding state; idle-timer-reset and superseded-push and cancellation and rollback fixes; corrected `skill.go`.
> **Effort**: Large
> **Parallel**: YES - 4 waves
> **Critical Path**: Wave 1 (types + config schema + hook rewrite) -> Wave 2 (gate init/doctor/ref-policy) -> Wave 3 (RPCs + orchestrator) -> Wave 4 (param derivation + skill fix + rollback policy)

## Context

### Original Request
Close the gap plans/made-rewrite.md's Final Verification Wave found: F1's "full pipeline end to end" and "failing test blocks push" QA scenarios, and F3's live `--mode made` cycle, could not be executed because nothing wires the 9 individually-tested pipeline stages into a real run. `grep -rln "runManager.Submit\|\.Submit(" --include="*.go" .` (excluding `_test.go`) returns zero matches; `cmd/made/main.go` has no `gate` subcommand at all.

### Interview Summary
Grounded via direct source reads and a librarian pass on no-mistakes' real push-admission model (not copied - independently synthesized) before asking anything:

- **Push model confirmed async, matching what's already built**: no-mistakes' pre-receive hook (`no-mistakes/internal/git/hook.go:51-56`) synchronously calls the daemon for a fast admission check only; post-receive (`hook.go:123-124,136-139,179,207`) always exits 0 and fires a non-blocking notify; the actual pipeline launches in a background goroutine the caller never waits on (`no-mistakes/internal/daemon/manager.go:611-612,959-961`). made's own `RunManager.Submit` (`internal/daemon/runmanager.go:90-123`) already behaves exactly this way - queues, returns immediately, drains in its own goroutine. **Decision: this is correct, keep it. `internal/skill/skill.go`'s claim that "the push blocks until the pipeline reaches a terminal state" is wrong and gets corrected in this plan, not the other way around.**
- **Hook auth: drop the shared-secret token, socket-only (0600) trust.** Verified herdr's own socket API (`herdr/src/api/server.rs:27`, `SOCKET_PERMISSION_MODE = 0o600`) has no token/secret layer anywhere in `ipc.rs`/`server.rs` - filesystem permissions are the sole boundary. made's own `internal/api` (already built, Task 6 of the prior plan) already follows this exact idiom. Keeping the hook's token would be the only inconsistent trust layer in the whole system. Metis confirmed zero non-test callers of `gitgate.InstallAdmissionHook` exist, so the signature can change freely, and confirmed consigliere's already-open PR #76 (branch `made-migration`, 4,842-line diff) has zero references to the token or the old signature - dropping it breaks nothing there.
- **`made runs` / `made axi abort` (forward-referenced by PR #76's `cs-made-lib.sh`/`cs-made-run-lib.sh`): deferred to a named follow-up plan, explicitly out of scope here.** PR #76 already documents both as forward references in its own header comments and degrades gracefully without them.
- **Hardening scope: fix idle-timer-reset, superseded-push handling, per-run/per-stage cancellation, AND a rollback/partial-failure policy now.** Explicitly deferred: run persistence across daemon restarts (stays in-memory, matching the prior plan's original design intent - a bigger architecture change, not silently corrupting state the way the other four gaps would).

### Metis Review (gaps addressed)
Metis (`omo:metis`) ground-checked every claim in the draft against real source and found 5 contradictions, 6 ambiguities, 12 missing constraints, and 9 execution risks. All resolved below, each tied to the task that closes it:

- **Contradiction - async vs skill.go's blocking claim**: resolved by interview above; Task 15 corrects `skill.go`.
- **Contradiction - `Result.OK` gating alone defeats the ask-user park semantics** that `skill.go`, `made review`, and consigliere's `cs-crew-state.sh` (verified via `gh pr diff 76`: "a non-empty `pending_findings[]` ... overrides a `running` state to parked") all depend on. Review/Document stages return `OK: true` even with pending findings (`internal/pipeline/review/review.go:76-81`, `internal/pipeline/document/document.go:69-73`) - the orchestrator must check `len(PendingFindings) > 0` as a SEPARATE park condition from `Result.OK`, not treat OK as "proceed unconditionally." See Task 12, Task 13.
- **Contradiction - consigliere's CI-green-but-still-open semantics**: PR #76's `cs-crew-state.sh` treats the `ci` stage's own `result == "pass"` as overriding a `running` state to done, while the run itself stays open pending human merge. The orchestrator must keep the run's top-level `state` at `running` (not `completed`) after CI passes, since PR merge is a human decision outside made's authority (made's PR stage structurally cannot merge, per the prior plan's Task 18). Only a stage failure or an explicit terminal condition moves `state` to `failed`; there is no automatic `state: completed` transition at all in v1 - a human merging the PR is the real terminal event, which made cannot observe. See Task 12's acceptance criteria.
- **Contradiction - trusted-config rule (d) as prose vs. as implemented**: the prior plan's prose said a missing trusted file is an error; `internal/config/config.go:103-105` already ships and tests treating a missing file as "trusted absent, no error, executable fields empty" (not a malformed/unreadable-but-present file, which does fail closed). **Resolution: `config.go`'s shipped, tested behavior is authoritative - a fresh gate's first run before any trusted branch is fetched is a normal "no trusted copy yet" state, not an error.** The prose gets corrected, not the code. See Task 6 for exactly when a trusted copy starts existing.
- **Ambiguity - what does `gate.admitPush` actually check under socket-only trust?** There's no identity to authenticate once the 0600 socket is the boundary - the check degrades to "is the daemon reachable and does it recognize this gate." Task 9 defines this precisely: reachable + gate path known to the daemon (verified via `gitgate.GatePath`) = admit; anything else = fail closed with a clear stderr message.
- **Ambiguity - ref policy**: nothing today reads pre-receive's stdin ref list or defines which refs a gate accepts. Task 8 defines and implements: `refs/heads/*` except the gate's configured default branch is accepted (creates a run); `refs/tags/*` and any other ref namespace is rejected with a clear message; a ref deletion (new-sha all zeros) is accepted but creates no run.
- **Ambiguity - `made.real-remote` name vs URL**: `push.Run(ctx, worktreePath, remoteName, branch)` (`internal/pipeline/push/push.go:41-47`) takes a remote **name**, and `pr.Run` resolves the GitHub repo from the worktree's own remotes via `gh`. **Resolution: `made gate init` both sets `made.real-remote` as a git-config URL value in the bare repo AND creates an actual named git remote (`origin`) in the bare gate repo pointing at that URL**, so worktrees cut from it inherit a real, usable `origin` remote and `push.Run`/`pr.Run` need no special-casing. See Task 6.
- **Ambiguity - trusted default-branch checkout has no source anywhere**: this is net-new design, not wiring. **Resolution**: `made gate init` fetches the real remote's default branch into the bare gate repo as its own tracked `refs/heads/<default>` (Task 6). The orchestrator resolves the trusted config by running `git show refs/heads/<default>:.made.yml` inside the bare repo into a temp file, passed as `LoadEffectiveConfig`'s `trustedPath` (Task 11). If that ref or path doesn't exist, it's the normal "no trusted copy" case per the contradiction resolution above, not an error.
- **Ambiguity - `config.Commands.Test`/`Lint` are `string`, stage functions want `[]string`**: resolved by tokenizing via `sh -c '<command>'` (i.e. `[]string{"sh", "-c", cfg.Commands.Test}`) - preserves shell semantics (pipes, globs) users expect from a single config string, and keeps the trust boundary identical to today (the string itself is still subject to the existing trusted-vs-pushed rules, only its *execution shape* changes). See Task 14.
- **Ambiguity - unrecognized/empty `config.Agent`**: fail closed with a clear error naming the invalid value, per the trusted-config boundary's existing fail-closed spirit - no silent default agent. See Task 14.
- **Missing constraint - idle timer kills in-flight runs** (`internal/daemon/lifecycle.go:41-44`, one-shot 30-min timer, default in `cmd/made/daemon.go:16`): Task 4 makes the timer reset on `RunManager` activity.
- **Missing constraint - uncancellable WorkFunc context** (`runmanager.go:158` calls `work(context.Background(), emit)`): Task 5 adds a per-run cancellation path and a per-stage deadline (CI polling in particular has no bound today, `internal/pipeline/ci/ci.go`).
- **Missing constraint - `reviewDecisions` store unreachable from the orchestrator** (`cmd/made/daemon.go:120` constructs it inline, captured only by the two review RPC handlers; `cmd/made/review.go:22-25` already self-flags this as a stopgap): Task 13 restructures ownership so the orchestrator's WorkFunc can wait on the same store the `review.decide`/`review.decision` RPCs use.
- **Missing constraint - `evidence.Config`, `ci.Run`'s budget/interval, `pr.Options` fields, `github.Client` construction all have no defined source**: Task 2 extends `config.Config` with the missing schema fields; Task 14 derives every stage-call parameter explicitly (see its own References section).
- **Missing constraint - hook script can't rely on inheriting `MADE_HOME`/`PATH` from git's hook-invocation environment**: Task 3 bakes the resolved absolute `made` binary path and `MADE_HOME` into the generated hook script text at install time.
- **Missing constraint - superseded pushes**: two quick pushes to the same branch would otherwise validate the same final ref against stale captured intent. Task 10 captures the pushed SHA at notify-push time and drops/supersedes a still-queued (not yet started) run for the same branch when a newer push for that branch arrives.
- **Missing constraint - repo identity key consistency**: `RunManager.Submit`'s `repo` argument must match `gitgate.GatePath`'s identifier convention or per-repo serialization silently degrades. Task 10 uses `gitgate.GatePath`'s repo-hash identifier as the canonical key everywhere.
- **Execution risk - `hook_test.go`'s three tests are entirely token-semantics** (`TestAdmissionHookRejectsPushWithoutToken`, `...WithWrongToken`, `...AcceptsPushWithValidToken`) and `testhelpers_test.go`'s `pushWithToken` helper exists only to set the token env var: Task 3 explicitly replaces these three tests with daemon-reachability-based fail-closed tests rather than trying to preserve token semantics that no longer exist, and drops the token argument from `worktree_test.go`'s `InstallAdmissionHook` calls.
- **Execution risk - F1's own QA scenario is unachievable as originally written**: "a trivial passing commit" ignores that `intent.Check` requires an `Intent:` trailer (`internal/pipeline/intent/intent.go:31-36`); "All 9 stages report success" has no wait/poll step against the now-confirmed-async model. This plan's own Final Verification Wave (F1) restates the scenario correctly: a commit with an explicit `Intent: <summary>` trailer, and a bounded poll (every 2s, up to 300s) on `made status --json` before asserting terminal state.
- **Execution risk - nothing has ever called `InstallAdmissionHook` outside a test** (`hook_test.go`/`worktree_test.go` are the only callers today): Task 6 (`made gate init`) is its first real caller, and gets first-class QA of its own rather than being a sub-bullet of a CLI task.
- **Execution risk - `made doctor` doesn't check gate initialization**, but PR #76's bootstrap prose already tells users to run it for exactly that: Task 7 adds a gate-init check to `made doctor`.
- **Topology gap - `StageResult`/`AskUserFinding` currently live in package `main`** (`cmd/made/status.go:52-60`), inverting the dependency direction a `daemon.RunSnapshot` extension would need: Task 1 relocates them to `internal/daemon`, preserving their JSON tags exactly so consigliere's `jq '.stages[].result'`-style reads are unaffected.

## Work Objectives

### Core Objective
Wire made's 9 already-tested pipeline stages into one real, socket-authenticated, git-push-triggered run, closing the exact gap plans/made-rewrite.md's F1/F3 found - without breaking anything that plan or consigliere's PR #76 already shipped.

### Deliverables
- `internal/gitgate`: socket-only admission hook (no token), pre-receive + new post-receive scripts baking the resolved `made` binary path and `MADE_HOME`.
- `cmd/made`: `gate init`, `gate admit-push`, `gate notify-push` subcommands; `doctor` gains a gate-init check.
- `internal/api` handlers: `gate.admitPush`, `gate.notifyPush` registered alongside the existing `status`/`review.*` handlers.
- `internal/orchestrator`: the WorkFunc builder chaining all 9 stages with real park/resume, cleanup, and cancellation.
- `internal/daemon`: relocated `StageResult`/`AskUserFinding` types, extended `RunSnapshot`, idle-timer-reset, per-run cancellation, restructured `reviewDecisions` ownership.
- `internal/config`: extended schema for evidence mode, CI rerun budget, command tokenization, agent-kind mapping.
- `internal/skill`: corrected async-model description, regenerated `skills/made/SKILL.md`.

### Definition of Done (verifiable conditions with commands)
- `cd made && go build ./... && go test ./...` exits 0.
- In a scratch repo with a real `gh`-authenticated fixture, `made gate init` then a commit carrying an `Intent: <summary>` trailer then `git push made <branch>` returns immediately (exit 0); polling `made status --json` every 2s for up to 300s reaches `state: running` with all 9 `stages[].result == "pass"` and a `pr` field/PR URL reachable via `gh pr view --json state` showing `OPEN`, never `MERGED`.
- The same fixture with a failing test commit: push still exits 0; polling reaches `state: failed` with `stages[].name=="test"` `result=="fail"`; `git ls-remote <real-remote>` shows no new ref.

### Must Have
- Every one of Metis's contradictions and missing constraints listed above has a task closing it.
- The ask-user park/resume path is real and independently testable end to end for the first time (the prior plan's Task 22 could only test it against a fixture `StatusReport`, never a real orchestrated run).
- Idle timer and cancellation fixes verified by a real test that starts a run, waits past the old timeout, and confirms the daemon is still alive.

### Must NOT Have
- No change to made's PR stage's structural inability to merge (untouched, already correct).
- No `made runs`/`made axi abort` CLI additions (explicitly deferred).
- No run-persistence-across-restart implementation (explicitly deferred, documented as a known limitation).
- No re-litigating the prior plan's trimmed adapter matrix (still GitHub + Claude/Codex only).
- No vendoring or copying no-mistakes' or herdr's actual code - independent synthesis, same principle as the prior plan.

## Verification Strategy
> ZERO HUMAN INTERVENTION - all verification is agent-executed.
- Test decision: TDD (RED-GREEN-REFACTOR), same convention as the prior plan.
- QA policy: every task has agent-executed scenarios, real invocations not `--dry-run`.
- Evidence: `evidence/task-{N}-{slug}.{ext}`, same directory as the prior plan (do not delete or overwrite its existing evidence files).

## Execution Strategy

### Parallel Execution Waves

**Wave 1 - Foundations** (parallel, no cross-dependencies): Tasks 1, 2, 3, 4, 5
**Wave 2 - Gate lifecycle** (depend on Wave 1): Tasks 6, 7, 8
**Wave 3 - RPCs + orchestrator** (depend on Wave 2): Tasks 9, 10, 11, 12, 13
**Wave 4 - Wiring finish** (depend on Wave 3): Tasks 14, 15, 16
**Final Verification Wave**: F1-F4

### Dependency Matrix (full, all tasks)

| Task | Depends On | Blocks |
|---|---|---|
| 1. Relocate StageResult/AskUserFinding, extend RunSnapshot | - | 12, 14 |
| 2. Extend config schema (evidence/CI/commands/agent) | - | 6, 14 |
| 3. Rewrite gitgate hook (drop token, socket-only, bake paths) | - | 6, 9, 10 |
| 4. Idle-timer reset on RunManager activity | - | 12 |
| 5. Per-run/per-stage cancellation | - | 12 |
| 6. `made gate init` (bare repo, remote, default-branch fetch, hooks) | 2, 3 | 7, 9, 10, 11 |
| 7. `made doctor` gate-init check | 6 | F1 |
| 8. Ref policy (accept/reject/delete) | 3 | 10 |
| 9. `gate.admitPush` RPC + `made gate admit-push` | 3, 6 | F1 |
| 10. `gate.notifyPush` RPC + `made gate notify-push` (SHA capture, supersede) | 3, 6, 8, 11 | 12 |
| 11. internal/orchestrator scaffold (config/worktree/visibility/evidence/cleanup) | 1, 2, 6 | 12 |
| 12. internal/orchestrator stage chaining + park/resume | 1, 4, 5, 10, 11, 13 | 14, F1 |
| 13. Restructure reviewDecisions ownership (orchestrator-reachable) | - | 12 |
| 14. Stage-param derivation (pr/github/ci config mapping) | 1, 2, 12 | F1 |
| 15. Correct skill.go async description, regenerate SKILL.md | - | F1 |
| 16. Rollback/partial-failure policy (push-succeeded-but-PR/CI-failed) | 12, 15 | F1 |
| F1-F4 | all above | - |

## TODOs

- [x] 1. Relocate StageResult/AskUserFinding to internal/daemon, extend RunSnapshot

  **What to do**: Move `StageResult` and `AskUserFinding` type definitions from `cmd/made/status.go:52-60` (package `main`) into `internal/daemon` (new file, e.g. `internal/daemon/runstate.go`), preserving their exact JSON tags (`name`/`result`, `stage`/`message`) so consigliere's already-merged-or-open `jq '.stages[].result'`-style reads are unaffected. Extend `daemon.RunSnapshot` (`internal/daemon/runmanager.go`) with `Stages []StageResult` and `PendingFindings []AskUserFinding` fields. Add thread-safe methods on `*RunManager` (or on the `run` struct) to update these fields as a run progresses, e.g. `func (rm *RunManager) UpdateStages(id string, stages []StageResult) error` and `func (rm *RunManager) UpdatePendingFindings(id string, findings []AskUserFinding) error`, called by the orchestrator (Task 12) as stages complete. Update `cmd/made/status.go` to import these types from `internal/daemon` instead of defining them locally, and to populate `StatusReport.Stages`/`PendingFindings` from the real `RunSnapshot` fields instead of the current hardcoded placeholders (`status.go:104-107`, `status.go:125`).
  **Must NOT do**: Change any JSON field name/tag - consigliere's PR #76 already depends on the current shape.

  **Parallelization**: Can Parallel: YES | Wave 1 | Blocks: 12, 14 | Blocked By: none

  **References**:
  - Current placeholder location: `/Users/douglasjarquin/github/douglasjarquin/made/cmd/made/status.go:38-50` (StatusReport struct), `:52-60` (StageResult/AskUserFinding definitions), `:104-107` (hardcoded pending stages), `:125` (hardcoded empty findings).
  - RunSnapshot to extend: `/Users/douglasjarquin/github/douglasjarquin/made/internal/daemon/runmanager.go` (read the full file for the current `RunSnapshot`/`run` struct shape and existing locking pattern before adding fields/methods - match the existing mutex discipline exactly).

  **Acceptance Criteria**:
  - [ ] `go test ./internal/daemon/... ./cmd/made/...` passes, including a new test asserting `UpdateStages`/`UpdatePendingFindings` are visible via `Snapshot`/`List` and via `cmd/made status --json`.
  - [ ] `cmd/made/status.go` no longer defines `StageResult`/`AskUserFinding` - `go vet` confirms no duplicate type definitions.

  **QA Scenarios**:
  ```
  Scenario: Real stage update visible in status
    Tool: bash
    Steps: Submit a run, call UpdateStages with a real stage result, query made status --json for that run.
    Expected: The JSON output reflects the updated stage, not a placeholder.
    Evidence: evidence/task-1-real-stage-update.txt

  Scenario: JSON shape unchanged
    Tool: bash
    Steps: Diff the JSON schema (field names/types) before and after this task against a fixed sample StatusReport.
    Expected: Identical field names and types - only the underlying data source moved.
    Evidence: evidence/task-1-schema-unchanged.txt
  ```

  **Commit**: YES | Message: `refactor(daemon): relocate StageResult/AskUserFinding, extend RunSnapshot with real stage state` | Files: `internal/daemon/runstate.go`, `internal/daemon/runmanager.go`, `cmd/made/status.go`

- [x] 2. Extend config schema (evidence mode, CI budget, command/agent resolution)

  **What to do**: Extend `internal/config.Config` (read `internal/config/config.go` fully first for the exact current struct shape and the trusted-vs-pushed rule implementation before adding fields) with: extend the EXISTING `internal/config.Evidence` struct (`config.go:44-46`, currently only `Branch string`, nested under `Test.Evidence` at `config.go:40-42`) by adding `StoreInRepo bool` and `Dir string` fields to it directly - do not declare a second, differently-named Evidence-like type (matching `internal/evidence.Config`'s existing shape, per `internal/evidence/store.go:8-12`) so a repo's config can select evidence mode; a `CI.RerunBudget int` field (default 2 when unset/zero) alongside the existing `CI.Required` field. Add a small helper function (e.g. `func (c Config) TestCommand() []string` and `func (c Config) LintCommand() []string`) that tokenizes the existing string `Commands.Test`/`Commands.Lint` fields as `[]string{"sh", "-c", cmd}` when non-empty, `nil` when empty - do not change the underlying string fields themselves, add derivation helpers. Add a helper `func (c Config) AgentKind() (agent.Kind, error)` that maps the existing string `Agent` field to `agent.KindClaude`/`agent.KindCodex` (read `internal/agent/agent.go` for the exact `Kind` type and constants), returning a clear error naming the invalid value for anything else (including empty) - fail closed, no silent default.
  **Must NOT do**: Change the trusted-vs-pushed rule set itself (`Commands`/`Agent`/`Agents` trusted-only unless `allow_repo_commands`, the four rules already implemented and tested) - only add new fields/derivation helpers on top of it.

  **Parallelization**: Can Parallel: YES | Wave 1 | Blocks: 6, 14 | Blocked By: none

  **References**:
  - Current config struct and trust rules: `/Users/douglasjarquin/github/douglasjarquin/made/internal/config/config.go` (read fully - the four trust rules are implemented around the effective-config resolution logic; missing-file-is-not-an-error behavior is at `config.go:103-105`, keep this as-is per the Context section's contradiction-4 resolution).
  - Evidence config shape to match: `/Users/douglasjarquin/github/douglasjarquin/made/internal/evidence/store.go:8-12`.
  - Agent kind constants: `/Users/douglasjarquin/github/douglasjarquin/made/internal/agent/agent.go`.

  **Acceptance Criteria**:
  - [ ] `go test ./internal/config/...` passes with new tests for: `Evidence.StoreInRepo`/`Dir` round-trip through `LoadEffectiveConfig`, `CI.RerunBudget` defaulting to 2 when unset, `TestCommand()`/`LintCommand()` tokenization (including the empty-string-returns-nil case), `AgentKind()` mapping both valid values and failing closed on an invalid/empty one.
  - [ ] The four existing trusted-vs-pushed rule tests (from the prior plan's Task 3) still pass unmodified.

  **QA Scenarios**:
  ```
  Scenario: CI rerun budget defaults
    Tool: bash
    Steps: Load a config with no ci.rerun_budget set, read the resolved value.
    Expected: 2.
    Evidence: evidence/task-2-ci-budget-default.txt

  Scenario: Invalid agent fails closed
    Tool: bash
    Steps: Load a config with agent: "gpt4" (unrecognized), call AgentKind().
    Expected: Non-nil error naming "gpt4" as invalid, no silent fallback.
    Evidence: evidence/task-2-invalid-agent-fails-closed.txt
  ```

  **Commit**: YES | Message: `feat(config): add evidence/CI/command/agent derivation for the orchestrator` | Files: `internal/config/config.go`

- [x] 3. Rewrite gitgate admission hook for socket-only auth

  **What to do**: Remove the shared-secret token mechanism entirely from `internal/gitgate/hook.go`: delete `admissionTokenFile`, `pushTokenEnvVar`, and the token-comparison logic in `admissionHookScript()`. Change `InstallAdmissionHook`'s signature to drop the `token` parameter (e.g. `func InstallHooks(repoPath, madeBinaryPath, madeHome string) error` - rename to reflect it now installs both pre-receive AND post-receive, not just admission). The new pre-receive script shells `"<madeBinaryPath>" gate admit-push --gate "<repoPath>"` with `MADE_HOME=<madeHome>` set in its environment (baked into the script text at install time, per the Context section's hook-environment-baking resolution - do not rely on inherited env vars), exits with that command's exit code. The new post-receive script reads git's stdin protocol (lines of `<old-sha> <new-sha> <refname>`), and for each line shells `"<madeBinaryPath>" gate notify-push --gate "<repoPath>" --old "<old-sha>" --new "<new-sha>" --ref "<refname>"` with the same baked `MADE_HOME`, always exits 0 regardless of that command's result (fire-and-forget, matching no-mistakes' own post-receive design cited in Context).
  **Must NOT do**: Do not preserve any token-check code path "for compatibility" - full removal, per the Context section's confirmation that zero non-test callers and zero consigliere references exist.

  **Parallelization**: Can Parallel: YES | Wave 1 | Blocks: 6, 9, 10 | Blocked By: none

  **References**:
  - Current implementation to rewrite: `/Users/douglasjarquin/github/douglasjarquin/made/internal/gitgate/hook.go` (full file - `InstallAdmissionHook` at line 15, `admissionHookScript` at line 34).
  - Tests to REPLACE, not extend (per Context's execution-risk resolution): `/Users/douglasjarquin/github/douglasjarquin/made/internal/gitgate/hook_test.go` (`TestAdmissionHookRejectsPushWithoutToken`, `...WithWrongToken`, `...AcceptsPushWithValidToken` - all three are pure token-semantics, delete and replace with daemon-reachability-based fail-closed tests: e.g. a pre-receive script generated against a socket path with nothing listening rejects the push).
  - Caller to update (drop token arg): `/Users/douglasjarquin/github/douglasjarquin/made/internal/gitgate/worktree_test.go:18,49` (calls `InstallAdmissionHook(barePath, "s3cr3t")` only as setup - update to the new signature).
  - no-mistakes' post-receive stdin-protocol handling to independently re-derive (concept only): `/Users/douglasjarquin/github/oss/no-mistakes/internal/git/hook.go:123-124,136-139,179,207`.

  **Acceptance Criteria**:
  - [ ] `go test ./internal/gitgate/...` passes with the three token tests replaced by reachability-based equivalents, and `worktree_test.go` updated to the new signature.
  - [ ] The generated pre-receive script contains the resolved absolute `made` binary path and `MADE_HOME` value baked in as literal text (verify by reading the generated script content in a test, not just executing it).
  - [ ] `grep -n "MADE_PUSH_TOKEN\|admissionTokenFile\|pushTokenEnvVar" internal/gitgate/*.go` returns zero matches.

  **QA Scenarios**:
  ```
  Scenario: Pre-receive rejects when daemon unreachable
    Tool: bash
    Steps: Install hooks pointed at a socket path with nothing listening, attempt a push.
    Expected: Push rejected, clear message, no token involved anywhere.
    Evidence: evidence/task-3-daemon-unreachable-rejected.txt

  Scenario: Post-receive always exits 0
    Tool: bash
    Steps: Install hooks, trigger post-receive with the daemon unreachable (notify-push will fail).
    Expected: Post-receive script itself exits 0 regardless (git never sees a non-zero post-receive).
    Evidence: evidence/task-3-post-receive-always-zero.txt
  ```

  **Commit**: YES | Message: `feat(gitgate): drop shared-secret hook auth for socket-only trust, add post-receive` | Files: `internal/gitgate/hook.go`, `internal/gitgate/hook_test.go`, `internal/gitgate/worktree_test.go`

- [x] 4. Idle-timer reset on RunManager activity

  **What to do**: In `internal/daemon/lifecycle.go`, change the idle-timeout mechanism from a one-shot `time.NewTimer` (current: `lifecycle.go:41-44`, never reset) to one that resets whenever the daemon's `RunManager` reports activity. Add a way for `Run`'s caller to supply an activity signal - e.g. `Options` gains an `ActivityCh <-chan struct{}` field (or equivalent), and `RunManager.Submit`/`drain` (in `internal/daemon/runmanager.go`) sends on a channel exposed via a new method like `func (rm *RunManager) ActivitySignal() <-chan struct{}` whenever a run starts, progresses (each `emit` call), or completes. `Run`'s select loop resets the idle timer on every receive from this channel instead of only checking it once.
  **Must NOT do**: Change the idle-timeout default value or the daemon's other lifecycle behaviors (SIGTERM handling, lock acquisition) - only the timer-reset mechanism.

  **Parallelization**: Can Parallel: YES | Wave 1 | Blocks: 12 | Blocked By: none

  **References**:
  - Current one-shot timer: `/Users/douglasjarquin/github/douglasjarquin/made/internal/daemon/lifecycle.go:41-44` (read the full `Run` function for the exact select-loop structure before modifying).
  - Default timeout value (unchanged): `/Users/douglasjarquin/github/douglasjarquin/made/cmd/made/daemon.go:16` (`defaultIdleTimeout = 30 * time.Minute`).
  - RunManager to add the activity signal to: `/Users/douglasjarquin/github/douglasjarquin/made/internal/daemon/runmanager.go` (read `Submit`/`drain` fully first).

  **Acceptance Criteria**:
  - [ ] `go test ./internal/daemon/...` passes with a new test: start `Run` with a short idle timeout (e.g. 200ms), submit a run whose WorkFunc sleeps for 500ms while periodically emitting events, assert the daemon is still running after 500ms (would have died under the old one-shot timer).
  - [ ] A daemon with no run activity at all still idle-times-out at the configured duration (regression test against the existing behavior).

  **QA Scenarios**:
  ```
  Scenario: Active run survives past the idle timeout
    Tool: bash
    Steps: Start daemon with a 1s idle timeout, submit a run that takes 3s with periodic activity, check daemon status at 2s.
    Expected: Daemon still running at 2s.
    Evidence: evidence/task-4-active-run-survives.txt

  Scenario: Truly idle daemon still times out
    Tool: bash
    Steps: Start daemon with a 1s idle timeout, submit nothing, wait 2s.
    Expected: Daemon has stopped.
    Evidence: evidence/task-4-idle-still-times-out.txt
  ```

  **Commit**: YES | Message: `fix(daemon): reset idle timer on run activity instead of a one-shot timer` | Files: `internal/daemon/lifecycle.go`, `internal/daemon/runmanager.go`

- [x] 5. Per-run/per-stage cancellation

  **What to do**: In `internal/daemon/runmanager.go`, change `drain`'s call to `work(context.Background(), emit)` (currently uncancellable, `runmanager.go:158`) to instead derive a cancellable context per run, and expose a way to cancel a specific in-flight run, e.g. `func (rm *RunManager) Cancel(id string) error`. Wire `made daemon stop` (`cmd/made/daemon.go`'s `daemonStop`) to cancel all in-flight runs before the daemon process itself exits, so a `made daemon stop` doesn't leave an orphaned pipeline goroutine. Separately, add a per-stage deadline specifically for `ci.Run`'s polling loop (`internal/pipeline/ci/ci.go`, which today has no bound) - the orchestrator (Task 12) derives a `context.WithTimeout` for the CI stage specifically, using a duration from config or a sensible fixed default (document your choice, e.g. 30 minutes, since GitHub Actions runs can legitimately take a while).
  **Must NOT do**: Change `ci.Run`'s own function signature - it already takes a `context.Context` (`internal/pipeline/ci/ci.go`), the deadline is applied by the caller via `context.WithTimeout`, not by changing the stage's own code.

  **Parallelization**: Can Parallel: YES | Wave 1 | Blocks: 12 | Blocked By: none

  **References**:
  - Current uncancellable call: `/Users/douglasjarquin/github/douglasjarquin/made/internal/daemon/runmanager.go:158`.
  - `made daemon stop` to wire cancellation into: `/Users/douglasjarquin/github/douglasjarquin/made/cmd/made/daemon.go` (`daemonStop` function).
  - CI stage's existing context param (no code change needed there, just caller-side timeout): `/Users/douglasjarquin/github/douglasjarquin/made/internal/pipeline/ci/ci.go`.

  **Acceptance Criteria**:
  - [ ] `go test ./internal/daemon/...` passes with a new test: submit a long-running WorkFunc, call `Cancel(id)`, assert the WorkFunc's context is cancelled and the run's final status reflects cancellation (not silently "completed").
  - [ ] `made daemon stop` with an in-flight run cancels it before the process exits (verified via a real subprocess test, not just unit-level).

  **QA Scenarios**:
  ```
  Scenario: Explicit cancel stops a running WorkFunc
    Tool: bash
    Steps: Submit a run whose WorkFunc blocks on ctx.Done(), call Cancel, assert it unblocks promptly.
    Expected: WorkFunc returns within a bounded time after Cancel.
    Evidence: evidence/task-5-explicit-cancel.txt

  Scenario: daemon stop cancels in-flight runs
    Tool: bash
    Steps: Start real daemon subprocess, submit a long run via the socket, run `made daemon stop`.
    Expected: The run's WorkFunc observes cancellation before the daemon process exits.
    Evidence: evidence/task-5-daemon-stop-cancels.txt
  ```

  **Commit**: YES | Message: `feat(daemon): cancellable per-run context, wire made daemon stop to cancel in-flight runs` | Files: `internal/daemon/runmanager.go`, `cmd/made/daemon.go`

- [x] 6. `made gate init` CLI command

  **What to do**: Add a `gate` subcommand namespace to `cmd/made/main.go`'s dispatch switch, with an `init` action: `made gate init <target-repo-path> <real-remote-url>`. Implementation: resolve the gate's bare-repo path via `gitgate.GatePath(madeHome, repoIdentifier)` (derive `repoIdentifier` from `target-repo-path`, e.g. its resolved absolute path, matching `gitgate.GatePath`'s existing hashing convention - read `internal/gitgate/layout.go` for the exact function); call `gitgate.InitBare` to create the bare repo; create a real named git remote `origin` inside the bare repo pointing at `real-remote-url` (`git remote add origin <url>` run inside the bare repo via `internal/exec`), and additionally set `made.real-remote` as a git-config key to the same URL (belt-and-suspenders per the Context section's ambiguity resolution - the named remote is what `push.Run`/`pr.Run` actually use, the config key is for any future explicit lookup); fetch the real remote's default branch into the bare repo as its own tracked ref (`git fetch origin <default-branch>:refs/heads/<default-branch>` - resolve `<default-branch>` via `git remote show origin` or a `--default-branch` flag, your call on the simplest reliable approach, document which you chose); call `gitgate.InstallHooks` (Task 3's renamed function) with the resolved `made` binary path (`os.Executable()`) and `MADE_HOME`; add a `made` git remote in the **target repo** (`target-repo-path`) pointing at the bare gate repo's path, so a user can `git push made <branch>` from their real working copy.
  **Must NOT do**: Do not skip the default-branch fetch step - without it, Task 11's trusted-config resolution has nothing to read from on a fresh gate (this is the first real establishment of "what is the trusted checkout" that Context flagged as previously undefined).

  **Parallelization**: Can Parallel: NO (depends on Task 2's config helpers and Task 3's renamed hook installer) | Wave 2 | Blocks: 7, 9, 10, 11 | Blocked By: 2, 3

  **References**:
  - `gitgate.GatePath`/`InitBare`: `/Users/douglasjarquin/github/douglasjarquin/made/internal/gitgate/layout.go`, `/Users/douglasjarquin/github/douglasjarquin/made/internal/gitgate/bare.go`.
  - `gitgate.InstallHooks` (Task 3's renamed signature): `/Users/douglasjarquin/github/douglasjarquin/made/internal/gitgate/hook.go`.
  - `internal/exec` for shelling `git remote add`/`git fetch`: `/Users/douglasjarquin/github/douglasjarquin/made/internal/exec/exec.go`.
  - CLI dispatch pattern to extend: `/Users/douglasjarquin/github/douglasjarquin/made/cmd/made/main.go`.

  **Acceptance Criteria**:
  - [ ] `go test ./cmd/made/...` passes with a real end-to-end test: run `made gate init` against a real scratch repo with a real (local, file://) remote, then verify the bare repo exists, has an `origin` remote pointing at the real remote, has a `made.real-remote` config key, has `refs/heads/<default>` fetched, has pre-receive+post-receive hooks installed, and the target repo has a `made` remote pointing at the bare repo.
  - [ ] Running `made gate init` twice against the same target is either idempotent or fails with a clear "already initialized" message (your call which, document it).

  **QA Scenarios**:
  ```
  Scenario: Fresh gate init end to end
    Tool: bash
    Steps: Create a scratch source repo with a local "real remote", run made gate init against it.
    Expected: All the artifacts listed in acceptance criteria are verifiably present.
    Evidence: evidence/task-6-gate-init-e2e.txt

  Scenario: Re-init behavior is defined and safe
    Tool: bash
    Steps: Run made gate init twice against the same target.
    Expected: Documented, non-corrupting behavior (idempotent or clear refusal) - no partial/broken state.
    Evidence: evidence/task-6-reinit-behavior.txt
  ```

  **Commit**: YES | Message: `feat(cli): add made gate init` | Files: `cmd/made/gate.go`, `cmd/made/main.go`

- [x] 7. `made doctor` gate-initialization check

  **What to do**: Add a check to `runDoctorCommand` (`cmd/made/doctor.go`) that reports whether the current directory (or a target path, if `made doctor` grows a positional arg - your call, document it) is a `made`-managed gate: resolve `gitgate.GatePath` for the current directory and check whether the bare repo exists at that path. Report `gate: initialized` or `gate: not initialized (run made gate init)` alongside the existing daemon/gh/herdr checks, consistent with the existing report format.
  **Must NOT do**: Make gate-uninitialized a fatal/`healthy = false` condition - it's informational (a fresh checkout legitimately has no gate yet, same spirit as herdr's own informational-only report in the existing doctor command).

  **Parallelization**: Can Parallel: YES | Wave 2 | Blocks: F1 | Blocked By: 6

  **References**:
  - Existing doctor command to extend: `/Users/douglasjarquin/github/douglasjarquin/made/cmd/made/doctor.go` (read the existing daemon/gh/herdr check pattern, match its style exactly).
  - `gitgate.GatePath`: `/Users/douglasjarquin/github/douglasjarquin/made/internal/gitgate/layout.go`.
  - Consigliere's expectation this satisfies (context only, not a dependency): PR #76's `bin/cs-brief.sh` prose telling soldiers to "run `made doctor`; if it reports the repo is not initialized here, run `made gate init`."

  **Acceptance Criteria**:
  - [ ] `go test ./cmd/made/...` passes with tests for both the initialized and not-initialized gate states.
  - [ ] `made doctor` in a directory with no gate reports `gate: not initialized` without affecting the overall exit code (still 0 if daemon/gh/herdr are otherwise fine).

  **QA Scenarios**:
  ```
  Scenario: Uninitialized gate reported informationally
    Tool: bash
    Steps: Run made doctor in a fresh directory with no gate.
    Expected: Reports "gate: not initialized", exit code unaffected by this alone.
    Evidence: evidence/task-7-uninitialized-gate.txt

  Scenario: Initialized gate reported
    Tool: bash
    Steps: Run made gate init, then made doctor in the same directory.
    Expected: Reports "gate: initialized".
    Evidence: evidence/task-7-initialized-gate.txt
  ```

  **Commit**: YES | Message: `feat(cli): made doctor reports gate initialization status` | Files: `cmd/made/doctor.go`

- [x] 8. Ref policy (accept/reject/delete)

  **What to do**: Implement a small, named, independently-testable function (e.g. `func ClassifyRef(ref, defaultBranch, oldSHA, newSHA string) RefDecision` in a new `internal/gitgate/refpolicy.go`, or wherever fits best - your call, but it must be a standalone unit Task 10 can call, not inline logic buried in the notify-push handler) implementing: `refs/heads/*` other than the gate's configured default branch -> accept, create a run; the configured default branch itself -> reject with a clear message ("pushing the default branch to the gate is not a supported flow" - matches existing `skill.go` prose, verify this doesn't need its own correction); `refs/tags/*` and any other ref namespace -> reject with a clear message naming the ref; a ref deletion (`newSHA` is all zeros) -> accept the ref update itself (git already handles this) but explicitly produce no run.
  **Must NOT do**: Do not make this policy configurable per-repo in v1 - a fixed, universal policy is sufficient and simpler; if a real need for per-repo ref policy emerges later, that's a follow-up.

  **Parallelization**: Can Parallel: YES | Wave 2 | Blocks: 10 | Blocked By: 3

  **References**:
  - Existing default-branch-push prose to stay consistent with: `/Users/douglasjarquin/github/douglasjarquin/made/internal/skill/skill.go` (search for "default branch" - read the exact current wording before writing your reject message).
  - Git's ref-deletion convention (all-zeros SHA) - standard git hook protocol, no repo-specific reference needed.

  **Acceptance Criteria**:
  - [ ] `go test ./internal/gitgate/...` (or wherever you placed this) passes with cases for: a feature branch (accept), the default branch (reject with message), a tag ref (reject with message), a ref deletion (accept, no-run signal).

  **QA Scenarios**:
  ```
  Scenario: Feature branch accepted
    Tool: bash
    Steps: ClassifyRef("refs/heads/feature-x", "main", "<sha>", "<sha>").
    Expected: Accept, run-eligible.
    Evidence: evidence/task-8-feature-branch-accepted.txt

  Scenario: Default branch and tags rejected
    Tool: bash
    Steps: ClassifyRef("refs/heads/main", "main", ...) and ClassifyRef("refs/tags/v1", "main", ...).
    Expected: Both rejected with distinct, clear messages.
    Evidence: evidence/task-8-default-and-tags-rejected.txt
  ```

  **Commit**: YES | Message: `feat(gitgate): ref-acceptance policy for gate pushes` | Files: `internal/gitgate/refpolicy.go`

- [ ] 9. `gate.admitPush` RPC + `made gate admit-push` CLI

  **What to do**: Register a new `gate.admitPush` handler in `registerDaemonHandlers` (`cmd/made/daemon.go`) alongside the existing `status`/`review.*` handlers, following the same `internal/api.HandlerFunc` pattern. Params: `{GatePath string}`. The handler's fail-closed check (per Context's ambiguity resolution - there's no identity to authenticate under socket-only trust, so the check is "is this a gate the daemon recognizes"): verify the given `GatePath` resolves to a real bare repo on disk (e.g. `gitgate.ValidateBareRepository`-equivalent check, or simply `os.Stat` + `git rev-parse --is-bare-repository` - read what `internal/gitgate` already exposes for this before writing new validation code) - if it's not a real, valid bare gate repo, return an error. Add a hidden `made gate admit-push --gate <path>` CLI command (`cmd/made/gate.go`, extending Task 6's file) that dials the daemon socket, calls `gate.admitPush`, and exits 0 on success / non-zero with the error message on failure - this is what Task 3's pre-receive script shells.
  **Must NOT do**: Do not create a run or touch `RunManager` in this handler - admission is a fast pre-check only, per the async model's separation of admission from pipeline execution (Context section).

  **Parallelization**: Can Parallel: YES | Wave 3 | Blocks: F1 | Blocked By: 3, 6

  **References**:
  - Handler registration pattern to follow exactly: `/Users/douglasjarquin/github/douglasjarquin/made/cmd/made/daemon.go` (`registerDaemonHandlers`, `statusHandler`, `reviewDecideHandler` - match this shape).
  - Bare-repo validation helpers already in gitgate: `/Users/douglasjarquin/github/douglasjarquin/made/internal/gitgate/bare.go` (check what's exported before writing new validation logic).
  - Socket client pattern to follow: `/Users/douglasjarquin/github/douglasjarquin/made/cmd/made/doctor.go`'s `checkDaemon` function (dial + call pattern).

  **Acceptance Criteria**:
  - [ ] `go test ./internal/api/... ./cmd/made/...` passes with tests for: a valid gate path admits successfully; an invalid/nonexistent path is rejected; a daemon-unreachable dial fails cleanly (non-zero exit, clear message).

  **QA Scenarios**:
  ```
  Scenario: Valid gate admitted
    Tool: bash
    Steps: made gate init a real gate, run made gate admit-push --gate <path> against a running daemon.
    Expected: Exit 0.
    Evidence: evidence/task-9-valid-gate-admitted.txt

  Scenario: Invalid gate rejected
    Tool: bash
    Steps: made gate admit-push --gate /nonexistent against a running daemon.
    Expected: Non-zero exit, clear error message.
    Evidence: evidence/task-9-invalid-gate-rejected.txt
  ```

  **Commit**: YES | Message: `feat(api): gate.admitPush RPC and made gate admit-push CLI` | Files: `cmd/made/daemon.go`, `cmd/made/gate.go`

- [ ] 10. `gate.notifyPush` RPC + `made gate notify-push` CLI (SHA capture, supersede)

  **What to do**: Register a `gate.notifyPush` handler in `registerDaemonHandlers`. Params: `{GatePath, OldSHA, NewSHA, Ref string}`. Handler logic: apply Task 8's `ClassifyRef` - if rejected, return an error (post-receive ignores it anyway per the fire-and-forget design, but the response should still be correct for testability); if it's a ref deletion, return success with no run created; otherwise resolve the branch name from `Ref` (strip `refs/heads/` prefix), resolve the canonical repo identifier via `gitgate.GatePath`'s convention (Context's repo-identity-consistency resolution - this MUST match what `RunManager.Submit`'s `repo` argument uses), check `RunManager` for an existing QUEUED (not yet started) run for the same `(repo, branch)` pair and drop/replace it with this newer push's SHA (superseded-push handling - read `internal/daemon/runmanager.go`'s queue structure to implement this correctly, add a method if needed e.g. `func (rm *RunManager) SupersedeQueued(repo, branch string) `), then call `RunManager.Submit` with a new run ID and a `WorkFunc` built by Task 11/12's orchestrator for this exact `NewSHA` (not just the branch tip at execution time - pass the captured SHA through so the worktree checks out exactly what was pushed, closing the stale-SHA gap Context flagged). Return the new run ID. Add a hidden `made gate notify-push --gate <path> --old <sha> --new <sha> --ref <ref>` CLI command that dials the socket, calls `gate.notifyPush`, and always exits 0 regardless of the RPC's result (per the fire-and-forget post-receive design) - log the result to stderr for debuggability but never fail the git operation.
  **Must NOT do**: Do not let a superseded run's already-started (not just queued) execution be killed by this - only QUEUED-but-not-yet-drained runs for the same branch are superseded; a run already mid-pipeline runs to completion (simpler and safer than trying to cancel mid-flight for this case).

  **Parallelization**: Can Parallel: NO (depends on Task 8's ref policy and needs the orchestrator's WorkFunc-building entry point from Task 11) | Wave 3 | Blocks: 12 | Blocked By: 3, 6, 8, 11

  **References**:
  - `RunManager`'s queue structure to extend for supersession: `/Users/douglasjarquin/github/douglasjarquin/made/internal/daemon/runmanager.go` (read the `repoQueue`/`queuedJob` structs fully).
  - `ClassifyRef` from Task 8: `internal/gitgate/refpolicy.go`.
  - `gitgate.GatePath`'s identifier convention: `/Users/douglasjarquin/github/douglasjarquin/made/internal/gitgate/layout.go`.

  **Acceptance Criteria**:
  - [ ] `go test ./internal/daemon/... ./cmd/made/...` passes with tests for: a normal push creates a run; a rejected ref (per Task 8) creates no run; a ref deletion creates no run; two quick pushes to the same branch result in only the SECOND SHA being validated (the first queued run is superseded, not run).
  - [ ] `made gate notify-push` always exits 0 even when the daemon is unreachable.

  **QA Scenarios**:
  ```
  Scenario: Normal push creates a run
    Tool: bash
    Steps: Call gate.notifyPush with a real feature-branch ref update.
    Expected: A new run ID returned, RunManager shows it queued/running.
    Evidence: evidence/task-10-normal-push-creates-run.txt

  Scenario: Superseded push
    Tool: bash
    Steps: Call gate.notifyPush twice in quick succession for the same branch with different new-SHAs, before the first drains.
    Expected: Only one run actually executes, validating the SECOND (newer) SHA.
    Evidence: evidence/task-10-superseded-push.txt
  ```

  **Commit**: YES | Message: `feat(api): gate.notifyPush RPC with SHA capture and superseded-push handling` | Files: `cmd/made/daemon.go`, `cmd/made/gate.go`, `internal/daemon/runmanager.go`

- [ ] 11. internal/orchestrator scaffold (config/worktree/visibility/evidence/cleanup)

  **What to do**: Create `internal/orchestrator` with the run-setup/teardown scaffold a WorkFunc needs, independent of the actual 9-stage chaining (Task 12 builds that on top). Implement: trusted-config resolution per Context's ambiguity resolution - run `git show refs/heads/<default>:.made.yml` inside the bare gate repo into a temp file for the trusted path (empty/missing is the normal "no trusted copy yet" case, not an error, per the corrected rule-(d) understanding), and resolve the pushed config from the worktree's own `.made.yml` if present, then call `internal/config.LoadEffectiveConfig(trustedPath, pushedPath)`; cut a worktree for the exact pushed SHA via `gitgate.AddWorktree` (checking out the SHA directly, not just the branch tip, per Task 10's stale-SHA fix); open herdr pane visibility via `internal/pipeline.Open(ctx, runID)`; construct the evidence store via `internal/evidence.NewStore` using Task 2's new `Evidence` config fields; construct `internal/github.Client{Dir: worktreePath}`. Guarantee cleanup (worktree `Remove()`, visibility `Close()`) on every exit path including panic (use `defer` with `recover()` at the top of the WorkFunc, converting a panic into a failed-run result rather than crashing the daemon).
  **Must NOT do**: Do not implement the actual stage-calling sequence here - this task is setup/teardown only, Task 12 is the sequence.

  **Parallelization**: Can Parallel: NO (depends on config schema, relocated types, and gate init's default-branch fetch existing) | Wave 3 | Blocks: 12 | Blocked By: 1, 2, 6

  **References**:
  - `gitgate.AddWorktree`/`Remove`: `/Users/douglasjarquin/github/douglasjarquin/made/internal/gitgate/worktree.go`.
  - `internal/pipeline.Open`/`Close` (herdr visibility): `/Users/douglasjarquin/github/douglasjarquin/made/internal/pipeline/herdrview.go`.
  - `internal/evidence.NewStore`/`Config`: `/Users/douglasjarquin/github/douglasjarquin/made/internal/evidence/store.go`.
  - `internal/config.LoadEffectiveConfig`: `/Users/douglasjarquin/github/douglasjarquin/made/internal/config/config.go`.
  - `internal/github.Client`: `/Users/douglasjarquin/github/douglasjarquin/made/internal/github/client.go`.

  **Acceptance Criteria**:
  - [ ] `go test ./internal/orchestrator/...` passes with tests for: trusted config resolves correctly when `.made.yml` exists on the fetched default branch; resolves to "no trusted copy" cleanly when it doesn't; worktree is cut at the exact pushed SHA (not branch tip, verified by pushing a SHA that isn't the current tip and confirming the worktree HEAD matches the pushed SHA); cleanup runs even when a simulated panic occurs mid-setup.

  **QA Scenarios**:
  ```
  Scenario: Trusted config resolution, present and absent
    Tool: bash
    Steps: Run the scaffold against a gate with a fetched default branch containing .made.yml, and against one without.
    Expected: First resolves real trusted config; second resolves cleanly with no error, executable fields empty.
    Evidence: evidence/task-11-trusted-config-both-cases.txt

  Scenario: Panic-safe cleanup
    Tool: bash
    Steps: Inject a panic partway through scaffold setup in a test, verify worktree/visibility are still cleaned up.
    Expected: No leaked worktree directories or open panes after the panic is recovered.
    Evidence: evidence/task-11-panic-safe-cleanup.txt
  ```

  **Commit**: YES | Message: `feat(orchestrator): run scaffold - config resolution, worktree, visibility, evidence, cleanup` | Files: `internal/orchestrator/scaffold.go`

- [ ] 12. internal/orchestrator stage chaining + park/resume (the core of this plan)

  **What to do**: Build the actual `WorkFunc` on top of Task 11's scaffold, chaining all 9 stages in fixed order: `intent.Check` -> `rebase.Run` -> `review.Run` -> `test.Run` -> `document.Run` -> `lint.Run` -> `push.Run` -> `pr.Run` -> `ci.Run`. For each stage: call it, use Task 1's `UpdateStages` to record the real `StageResult{Name, Result}` on the run snapshot, `emit` a `stage_started`/`stage_finished` `Event`. Gate correctly per Context's contradiction-2 resolution: a stage's `Result.OK == false` halts the pipeline with a failed result naming the stage and message (this is the ONLY thing that produces `state: failed`). SEPARATELY, after Review and Document specifically, check `len(Result.PendingFindings) > 0` (or `.Findings` for Document) as its own condition: if non-empty, call Task 1's `UpdatePendingFindings`, keep the run's status as the existing `RunRunning` (do NOT introduce a new status value - matches consigliere's PR #76 expectation that a parked run still reads as `running` with populated `pending_findings[]`, per Context's contradiction-3 resolution), and BLOCK on Task 13's restructured `reviewDecisions` store until a decision arrives for every pending finding at that stage - a `rejected` decision fails the run (same as a stage `Result.OK == false`); all-`approved` proceeds to the next stage. After a successful CI stage (`Result.OK == true`), the run's top-level state stays `running` (per Context's contradiction-3: made cannot observe a human merging the PR, so there is no automatic `completed` transition in v1 - document this explicitly in the run's final message, e.g. "all stages passed, PR open, awaiting merge"). Apply Task 5's per-stage CI deadline via `context.WithTimeout` specifically around the `ci.Run` call.
  **Must NOT do**: Do not treat `Result.OK == true` alone as "proceed" for Review/Document - the separate pending-findings check is mandatory, per Context's contradiction-2. Do not introduce a `completed` run status for a CI-passed run - it stays `running` per contradiction-3.

  **Parallelization**: Can Parallel: NO (the single most load-bearing task in this plan) | Wave 3 | Blocks: 14, F1 | Blocked By: 1, 4, 5, 10, 11, 13

  **References**:
  - All 9 stage signatures (verified accurate by Metis against real source):
    - `intent.Check(repoPath string) (Result, error)` - `/Users/douglasjarquin/github/douglasjarquin/made/internal/pipeline/intent/intent.go`
    - `rebase.Run(worktreePath, defaultBranch string) (Result, error)` - `/Users/douglasjarquin/github/douglasjarquin/made/internal/pipeline/rebase/rebase.go`
    - `review.Run(ctx, worktreePath string, agentKind agent.Kind, opts review.Options) (Result, error)` - `/Users/douglasjarquin/github/douglasjarquin/made/internal/pipeline/review/review.go`
    - `test.Run(ctx, worktreePath, runID string, testCommand []string, store evidence.Store) (Result, error)` - `/Users/douglasjarquin/github/douglasjarquin/made/internal/pipeline/test/test.go`
    - `document.Run(worktreePath, baseBranch string, rules []document.Rule) (Result, error)` - `/Users/douglasjarquin/github/douglasjarquin/made/internal/pipeline/document/document.go`
    - `lint.Run(ctx, worktreePath, runID string, lintCommand []string, store evidence.Store) (Result, error)` - `/Users/douglasjarquin/github/douglasjarquin/made/internal/pipeline/lint/lint.go`
    - `push.Run(ctx, worktreePath, remoteName, branch string) (Result, error)` - `/Users/douglasjarquin/github/douglasjarquin/made/internal/pipeline/push/push.go`
    - `pr.Run(ctx, ghClient *github.Client, opts pr.Options) (Result, error)` - `/Users/douglasjarquin/github/douglasjarquin/made/internal/pipeline/pr/pr.go`
    - `ci.Run(ctx, ghClient *github.Client, prURL string, rerunBudget int, pollInterval time.Duration) (Result, error)` - `/Users/douglasjarquin/github/douglasjarquin/made/internal/pipeline/ci/ci.go`
  - Park semantics source (why OK-alone is insufficient): `/Users/douglasjarquin/github/douglasjarquin/made/internal/pipeline/review/review.go:76-81`, `/Users/douglasjarquin/github/douglasjarquin/made/internal/pipeline/document/document.go:69-73`, `/Users/douglasjarquin/github/douglasjarquin/made/internal/skill/skill.go` (search "ask-user").
  - consigliere's expectation this must satisfy (context, verify via `gh pr diff 76` if accessible): PR #76's `bin/cs-crew-state.sh` header comments describing pending-findings-overrides-running and ci-pass-overrides-to-done-while-staying-open semantics.

  **Acceptance Criteria**:
  - [ ] `go test ./internal/orchestrator/...` passes with tests for: a fully-passing run reaches all 9 stages with real `StageResult`s, ends `state: running` with a final message noting PR-open-awaiting-merge (not `completed`); a Test-stage failure halts at Test with `state: failed`; a Document-stage finding parks the run (`state: running`, non-empty `PendingFindings`) and blocks until a decision arrives; a rejected decision at any parked stage fails the run; an approved decision resumes to the next stage.
  - [ ] The CI stage is bounded by Task 5's deadline (a CI stage that never terminates is force-failed at the deadline, not hung forever).

  **QA Scenarios**:
  ```
  Scenario: Full pass, real stage results
    Tool: bash
    Steps: Run the orchestrator's WorkFunc directly (not through the socket yet - that's F1) against a real worktree with a passing test/lint config.
    Expected: All 9 StageResults recorded as pass, final state running with PR-awaiting-merge message.
    Evidence: evidence/task-12-full-pass.txt

  Scenario: Document finding parks and resumes
    Tool: bash
    Steps: Run against a worktree that trips a Document rule, verify parked state, submit an approval via the reviewDecisions store, verify resumption to Lint.
    Expected: Correct park then resume, not a silent skip past the finding.
    Evidence: evidence/task-12-park-and-resume.txt
  ```

  **Commit**: YES | Message: `feat(orchestrator): chain all 9 pipeline stages with real park/resume semantics` | Files: `internal/orchestrator/workfunc.go`

- [ ] 13. Restructure reviewDecisions ownership (orchestrator-reachable)

  **What to do**: Move the `reviewDecisions` store (currently constructed inline inside `registerDaemonHandlers`, `cmd/made/daemon.go:120`, and captured only by the closures for `review.decide`/`review.decision`) to a location both the RPC handlers AND Task 12's orchestrator WorkFunc can reach - e.g. construct it once in `startDaemon` (`cmd/made/daemon.go`) and pass it into both `registerDaemonHandlers` and the orchestrator's `WorkFunc` builder (Task 12 needs a reference to call into it when checking for/waiting on a decision). Add a wait/notify mechanism the orchestrator can block on when parked (e.g. a per-`(runID, stage)` channel or a condition-variable-based wait, or reuse `RunManager`'s existing `Subscribe`/event-channel pattern if that's cleaner - your call, but it must let the orchestrator block efficiently rather than busy-polling). `cmd/made/review.go:22-25` already self-documents this exact restructuring as a known stopgap - read that comment for the acknowledged intent before implementing.
  **Must NOT do**: Do not change `review.decide`/`review.decision`'s RPC params/response shape - only where the underlying store lives and how the orchestrator gets notified.

  **Parallelization**: Can Parallel: YES | Wave 3 | Blocks: 12 | Blocked By: none

  **References**:
  - Current inline construction and self-documented stopgap: `/Users/douglasjarquin/github/douglasjarquin/made/cmd/made/daemon.go:120`, `/Users/douglasjarquin/github/douglasjarquin/made/cmd/made/review.go:22-25`.
  - `RunManager`'s existing Subscribe/event pattern (reusable model for the wait mechanism): `/Users/douglasjarquin/github/douglasjarquin/made/internal/daemon/runmanager.go`, `/Users/douglasjarquin/github/douglasjarquin/made/internal/daemon/mailbox.go`.

  **Acceptance Criteria**:
  - [ ] `go test ./cmd/made/... ./internal/orchestrator/...` passes with a test proving a decision recorded via the `review.decide` RPC path is observable by a concurrently-blocked orchestrator-side waiter (real concurrency test, not sequential call-then-check).
  - [ ] Existing `review.decide`/`review.decision` RPC tests from the prior plan's Task 22 still pass unmodified in their assertions (only the store's construction location changed).

  **QA Scenarios**:
  ```
  Scenario: Cross-path decision visibility
    Tool: bash
    Steps: Start a goroutine blocked waiting on a decision for (runID, stage), call review.decide via the RPC path from another goroutine, assert the waiter unblocks with the right decision.
    Expected: Waiter receives the decision promptly, no polling delay beyond a reasonable bound.
    Evidence: evidence/task-13-cross-path-visibility.txt
  ```

  **Commit**: YES | Message: `refactor(cli): restructure reviewDecisions ownership to be orchestrator-reachable` | Files: `cmd/made/daemon.go`, `cmd/made/review.go`

- [ ] 14. Stage-param derivation (pr/github/ci config mapping)

  **What to do**: Implement the concrete parameter-derivation logic Task 12's WorkFunc calls into for the stages whose inputs Context flagged as previously undefined: `pr.Options{Title, Base, Head, EvidenceRef}` - `Title` from the pushed commit's subject line (`git log -1 --format=%s` in the worktree), `Base` from the gate's resolved default branch (established at `made gate init`, Task 6), `Head` from the pushed branch name, `EvidenceRef` from the evidence store's location for this run (a path or branch reference, matching whichever mode Task 2's `Evidence` config selected); `github.Client{Dir: worktreePath, Timeout: <a named constant, document your choice>}`; `ci.Run`'s `rerunBudget` from Task 2's `Config.CI.RerunBudget`, `pollInterval` as a fixed named constant (document your choice, e.g. 10 seconds); `test.Run`/`lint.Run`'s command arguments from Task 2's `TestCommand()`/`LintCommand()` helpers; `review.Run`'s `agentKind` from Task 2's `AgentKind()` helper (propagating its fail-closed error up as a run-level infra failure, not a stage `Result`, if the configured agent is invalid).
  **Must NOT do**: Do not hardcode any of these values directly in Task 12's WorkFunc - keep this derivation in its own clearly-named function(s) so it's independently testable against fixture configs.

  **Parallelization**: Can Parallel: NO (needs the real Task 12 stage-calling structure to plug into) | Wave 4 | Blocks: F1 | Blocked By: 1, 2, 12

  **References**:
  - `pr.Options`/`github.Client`/`ci.Run` signatures: same files cited in Task 12's References.
  - Config helpers this derives from: `internal/config/config.go` (Task 2's additions).

  **Acceptance Criteria**:
  - [ ] `go test ./internal/orchestrator/...` passes with tests for each derived parameter against a fixture config and a fixture worktree with a known commit subject/branch name.
  - [ ] An invalid `AgentKind()` configuration surfaces as a clear run-level error (not a stage failure, not a panic).

  **QA Scenarios**:
  ```
  Scenario: PR title/base/head derived correctly
    Tool: bash
    Steps: Run derivation against a worktree with a known commit subject and branch name, a gate with a known default branch.
    Expected: pr.Options fields match exactly.
    Evidence: evidence/task-14-pr-options-derived.txt
  ```

  **Commit**: YES | Message: `feat(orchestrator): derive per-stage parameters from resolved config` | Files: `internal/orchestrator/params.go`

- [ ] 15. Correct skill.go's async description, regenerate SKILL.md

  **What to do**: Update `internal/skill/skill.go`'s body text (search for "the push blocks until the pipeline reaches a terminal state") to accurately describe the real, async model established by this plan: a push returns immediately once admitted; the pipeline runs in the background; the user/agent watches progress via `made status --json` (polling or, if Task 13's wait mechanism is exposed at the CLI level, mention that too) and `made review` for any parked findings. Also verify and, if needed, correct the "pushing the default branch to the gate is not a supported flow" prose against Task 8's actual `ClassifyRef` rejection message so they match exactly. Regenerate `skills/made/SKILL.md` via `make skill` (or `go run ./cmd/genskill`), matching the prior plan's Task 23 generated-skill-file discipline (source of truth is the Go constant, the committed file is always regenerated, never hand-edited).
  **Must NOT do**: Do not hand-edit `skills/made/SKILL.md` directly - regenerate it from the corrected `skill.go` source, per the existing drift-lint test from the prior plan's Task 23.

  **Parallelization**: Can Parallel: YES | Wave 4 | Blocks: F1 | Blocked By: none

  **References**:
  - File to correct: `/Users/douglasjarquin/github/douglasjarquin/made/internal/skill/skill.go`.
  - Existing drift-lint test that will catch a forgotten regeneration: `/Users/douglasjarquin/github/douglasjarquin/made/internal/skill/skill_test.go` (`TestCommittedSkillFileMatchesGenerator`, from the prior plan's Task 23).

  **Acceptance Criteria**:
  - [ ] `go test ./internal/skill/...` passes (including the existing drift-lint test against the regenerated file).
  - [ ] `grep -n "blocks until" internal/skill/skill.go` returns zero matches (the corrected text no longer claims blocking behavior).

  **QA Scenarios**:
  ```
  Scenario: Corrected skill file regenerated and drift-clean
    Tool: bash
    Steps: Edit skill.go's body, run make skill, run go test ./internal/skill/....
    Expected: skills/made/SKILL.md matches the corrected generator output exactly, drift test passes.
    Evidence: evidence/task-15-corrected-skill-regenerated.txt
  ```

  **Commit**: YES | Message: `docs(skill): correct async push model description, regenerate SKILL.md` | Files: `internal/skill/skill.go`, `skills/made/SKILL.md`

- [ ] 16. Rollback/partial-failure policy (push succeeded, PR/CI failed)

  **What to do**: Define and implement what happens when `push.Run` (stage 7) succeeds - the validated branch is now on the real remote - but `pr.Run` (stage 8) or `ci.Run` (stage 9) then fails. Made has no authority to force-revert a real remote (per the prior plan's Must-NOT-Have constraints and the trimmed-scope philosophy), so the policy is: the run reports `state: failed` with a message that explicitly states the branch WAS already pushed to the real remote and names which later stage failed (e.g. "push succeeded (branch now on origin), but PR creation failed: <reason> - the branch is live on the real remote, no automatic action taken"). Surface this distinction in `skill.go`'s outcomes section too (Task 15's file) so an agent reading the skill understands a "failed" run doesn't always mean nothing happened.
  **Must NOT do**: Do not attempt to auto-revert, force-push, or delete the already-pushed branch - that is exactly the kind of destructive, authority-exceeding action this whole system is designed to avoid.

  **Parallelization**: Can Parallel: NO (depends on Task 12's real stage sequencing to attach this message logic to; run after Task 15 since both edit internal/skill/skill.go and Task 16's outcomes-section wording must build on Task 15's corrected async description, not race it) | Wave 4 | Blocks: F1 | Blocked By: 12, 15

  **References**:
  - Stage sequence this attaches to: Task 12's `internal/orchestrator/workfunc.go`.
  - Outcomes section to update: `/Users/douglasjarquin/github/douglasjarquin/made/internal/skill/skill.go` (Task 15's file - coordinate the wording).

  **Acceptance Criteria**:
  - [ ] `go test ./internal/orchestrator/...` passes with a test: push succeeds, PR stage is made to fail (e.g. via a fixture `gh` failure), the run's final message explicitly states the branch is live on the real remote.

  **QA Scenarios**:
  ```
  Scenario: Push-succeeded-PR-failed message is explicit
    Tool: bash
    Steps: Run the orchestrator with a fixture that makes PR creation fail after a real push.
    Expected: Final run message names the pushed branch as live on the real remote and the specific later failure.
    Evidence: evidence/task-16-push-succeeded-pr-failed-message.txt
  ```

  **Commit**: YES | Message: `feat(orchestrator): explicit rollback/partial-failure policy for push-then-fail` | Files: `internal/orchestrator/workfunc.go`, `internal/skill/skill.go`

## Final Verification Wave (MANDATORY - after ALL implementation tasks)
> ALL must APPROVE. Present consolidated results to user and get explicit "okay" before completing.
- [ ] F1. Plan Compliance Audit

  **What to do**: Re-execute every Definition of Done command for real. Confirm every Must Have present, every Must NOT Have absent.
  **Must NOT do**: Trust task self-reports - re-run the commands yourself.

  **Acceptance Criteria**:
  - [ ] `go build ./... && go test ./...` exits 0.
  - [ ] The corrected end-to-end scenario (Intent-trailer commit, bounded poll, real GitHub fixture) passes exactly as stated in Definition of Done.
  - [ ] The corrected failing-test scenario passes exactly as stated in Definition of Done.
  - [ ] `grep -rn "runManager.Submit\|\.Submit(" --include="*.go" . | grep -v _test.go` now returns at least one production match (the orchestrator itself).

  **QA Scenarios**:
  ```
  Scenario: Full pipeline end to end, corrected
    Tool: bash
    Steps: made gate init against a real gh-authenticated scratch repo; commit with an Intent trailer; git push made <branch>; poll made status --json every 2s up to 300s.
    Expected: state reaches running with all 9 stages[].result == pass; gh pr view --json state shows OPEN, never MERGED.
    Evidence: evidence/task-F1-full-pipeline.txt

  Scenario: Failing test blocks, corrected
    Tool: bash
    Steps: Same fixture, a commit with an Intent trailer and a failing test; git push made <branch>; poll as above.
    Expected: state reaches failed; stages[].name=="test" result=="fail"; git ls-remote <real-remote> unchanged.
    Evidence: evidence/task-F1-failing-test-block.txt
  ```

  **Commit**: NO

- [ ] F2. Code Quality Review

  **What to do**: `golangci-lint run --max-issues-per-linter=0 --max-same-issues=0 ./...` across the whole made module (the prior plan's F2 found 30 real issues hiding behind default issue caps - use the uncapped flags this time from the start).
  **Must NOT do**: Blanket `//nolint` - fix or justify each finding explicitly, same discipline as the prior plan's F2.

  **Acceptance Criteria**:
  - [ ] `golangci-lint run --max-issues-per-linter=0 --max-same-issues=0 ./...` reports zero issues.

  **QA Scenarios**:
  ```
  Scenario: Lint clean run
    Tool: bash
    Steps: Run the uncapped golangci-lint command above.
    Expected: Zero findings.
    Evidence: evidence/task-F2-lint-clean.txt
  ```

  **Commit**: NO

- [ ] F3. Real Manual QA

  **What to do**: Actually run the ask-user park/resume flow against a REAL orchestrated run for the first time (previously only testable against a fixture StatusReport, per the prior plan's Task 22), and re-confirm herdr fail-open still holds with the orchestrator wired in.
  **Must NOT do**: Substitute the F1 scenario's output for this - park/resume needs its own real trigger (a review/document finding), not just a passing run.

  **Acceptance Criteria**:
  - [ ] A real push whose diff triggers a genuine ask-user finding (e.g. a Document-stage policy violation) produces a real parked run; `made review` against it shows the real finding; approving resumes the run to completion, rejecting halts it.
  - [ ] With herdr running, a real orchestrated run's pane is visible via `herdr pane list --session made`; with herdr stopped, the identical run completes identically (fail-open, now proven against real orchestrated stages, not just the Task 20 fixture).

  **QA Scenarios**:
  ```
  Scenario: Real park and resume
    Tool: bash
    Steps: Push a branch that trips a Document-stage rule; made review on the resulting run; approve.
    Expected: Run resumes past Document and completes; evidence shows the real approval.
    Evidence: evidence/task-F3-real-park-resume.txt

  Scenario: herdr fail-open against a real run
    Tool: bash
    Steps: Run the F1 full-pipeline scenario twice, once with herdr up, once down.
    Expected: Identical stage outcomes both times.
    Evidence: evidence/task-F3-herdr-fail-open-real.txt
  ```

  **Commit**: NO

- [ ] F4. Scope Fidelity Check

  **What to do**: Spot-check that internal/orchestrator is structurally independent from no-mistakes' internal/daemon/manager.go (the closest analog - it's the component that launches pipelines from a push event).
  **Must NOT do**: Treat "it passes F1" as sufficient - independence is a structural claim, not a behavioral one.

  **Acceptance Criteria**:
  - [ ] Side-by-side comparison of `internal/orchestrator/*.go` against `no-mistakes/internal/daemon/manager.go`'s push-handling section shows independent structure and naming.

  **QA Scenarios**:
  ```
  Scenario: Independence spot-check
    Tool: bash
    Steps: Diff structure and skim implementations side by side.
    Expected: No copied code, identifiers, or comments found.
    Evidence: evidence/task-F4-orchestrator-independence.md
  ```

  **Commit**: NO

## Commit Strategy
Each implementation task (1-16) commits independently once its acceptance criteria and QA scenarios pass, Conventional Commits (`feat(orchestrator): ...`, `fix(gitgate): ...`). Final Verification Wave tasks do not commit.

## Success Criteria
- A real `git push made <branch>` runs all 9 stages end to end, reports real per-stage results, opens a PR, and never merges it.
- Ask-user findings genuinely park and resume a real run, not just a fixture.
- The daemon survives a real CI-length run without idle-timing itself out, and a superseded push never validates against stale intent.
- `skill.go` accurately describes the system's real (async) behavior.

