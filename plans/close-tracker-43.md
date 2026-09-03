# Close issue #43: fix real bugs found by the live Cursor Cloud canary

## TL;DR
> **Summary**: Issues #38-#42 are all merged. This plan closes the remaining gap between that and #43's Definition of Done, driven entirely by real, reproduced findings from a live Cursor Cloud canary run (PR #55) rather than speculation.
> **Deliverables**: fixed `internal/cursor` skill/doctor content + regenerated `.cursor/` projections + drift test; a truthful stage-reporting fix shared by `made validate --managed` and `made verify`; `requested_model` propagation; `--json` exit-code and receipt-locator fixes; `capabilities` additions; an interim pinned-install script; PR #22 closed as superseded; PR #55 split and re-landed correctly; a follow-up live-canary re-verification; final DoD audit on #43.
> **Effort**: Large
> **Parallel**: YES - 4 waves
> **Critical Path**: Task 1 (split PR #55) -> Task 5 (fix skill.go) -> Task 7 (regenerate `.cursor/` + drift test) -> Task 12 (land fix branch) -> Task 13 (re-verify canary) -> Task 14 (final audit + close #43)

## Context

### Original Request
Continue work on GitHub issue #43 (tracker: daemonless Made verification for Cursor Cloud with optional review guides) via the Prometheus planning skill. Issues #38-#42 (the tracker's 5 phases) are already merged to `main`.

### Interview Summary
- User has real Cursor Cloud access. I gave them a canary prompt; they ran it and returned PR #55 with a detailed, verbatim agent comment covering 8 concrete technical findings plus process friction.
- User did not answer the "pinned install" preference question directly; per this skill's own rule (unanswered preference -> apply default, record as assumption), this plan includes a lightweight interim install script rather than waiting on issue #27's full release pipeline.

### Metis Review (gaps addressed)
Metis (full read of `internal/cursor`, `internal/verify`, `internal/managed`, `cmd/made`, and both PR #22 and PR #55) found 9 contradictions in the working draft, confirmed all 8 canary findings reproduce in real code (with 3 corrected file pointers), and surfaced 3 additional unlisted DoD gaps (`made_version` missing from `verify.Receipt`, no `reused` concept in one-shot receipts, pinned builds still report `version=dev`). All findings are incorporated into this plan's tasks and constraints below. See "Key Decisions" for the two calls Metis flagged as highest-priority.

## Work Objectives

### Core Objective
Fix every real, reproduced defect the live canary surfaced, land it cleanly (including untangling PR #55's stale generated files), give the user a second canary prompt to re-verify the fixes on real Cursor Cloud, and close out #43 with an honest final audit - explicitly marking what remains deliberately out of scope rather than claiming false completion.

### Deliverables
- `internal/cursor/skill.go`: corrected doctor branching (checks `detail`, not `status`; handles the `warn` case), `--base-ref` in the doctor step, and a rewritten step 4 that works when the calling harness has no native "invoke this custom subagent file" primitive.
- `internal/cursor/doctor.go`: `base_ref` check can never flip `healthy` to `false` on its own (stays informational).
- Regenerated `.cursor/agents/made-reviewer.md` and `.cursor/skills/verify-with-made/SKILL.md` from the fixed generator, plus a committed-file drift test analogous to `internal/skill`'s `TestCommittedSkillFileMatchesGenerator`.
- `internal/managed/runner.go`: stages that were planned to run but never reached (because an earlier stage produced a terminal, non-pass outcome) stay in `StageResults` at their initial `pending` status instead of being silently dropped. This is a versioned-contract change affecting both `made validate --managed`'s `terminal.json` and `made verify`'s receipts.
- `internal/verify/receipt.go`: `Receipt` gains `made_version`, `protocol_version`, `receipt_path`, and `evidence_dir` fields; `schema_version` bumped (no backward-compat shim - old cached receipts become unreadable, which is fine, they're disposable local cache).
- `internal/managed/testdata/*.jsonl` golden fixtures regenerated to match the new truthful stage reporting.
- `internal/verify/prepare.go`: `requested_model` is populated from `review.executors.<executor>.model` when `--executor` matches a configured executor and `--requested-model` wasn't explicitly given.
- `cmd/made/verify.go`: `--json` mode for `run` and `complete` exits with the same semantic exit code as human mode (0 passed / 1 infrastructure_error / 3 needs_decision / 4 failed_retryable / 5 failed_terminal / 130 canceled), and its JSON body includes the receipt/evidence locations.
- `cmd/made/runcommands.go`: `capabilities --json` additively lists `verify` and `cursor` as top-level commands; a new assertion test locks the exact tokens; `docs/managed-validation-v1.md` or a small new doc note mentions them.
- `scripts/install-cursor-cloud.sh`: builds and installs `made` pinned to an exact commit SHA (via `-ldflags "-X github.com/douglasjarquin/made/internal/managed.MadeVersion=<sha>"` so receipts stop showing `version=dev`), plus a pinned `golangci-lint` install (matching CI's `v2.11.2`), documented in the skill's Installation section.
- PR #22 closed, referencing the PRs that superseded it (#51-#54).
- PR #55 split: its `.made.yaml`/`.made/features/README.md`/README doc changes are kept; its stale `.cursor/` commit is dropped and replaced by freshly generated files once the skill.go fix lands on the same branch.
- A second, small canary prompt (this plan's own artifact) for the user to run on real Cursor Cloud, re-verifying items 1, 4, and 9 specifically (the three findings that are only provable in that environment).
- Issue #43 updated with a final, honest audit: which DoD boxes are genuinely closed, and which (the full ~15-scenario canary matrix across 2 repos; lane-reuse wiring inside managed mode) are explicitly, deliberately deferred as ongoing operational/future work - matching the precedent already set by #41 and #42's own Phase 4 deferrals.

### Definition of Done (verifiable conditions with commands)
- `go build ./...`, `go vet ./...`, `go test ./...`, `go test -race -shuffle=on -count=1 ./...` all clean at every wave boundary.
- `go test ./internal/cursor/... ./internal/managed/... ./internal/verify/... ./cmd/made/...` green, including all new tests this plan adds.
- A real, built `made` binary run against a real temp repo shows: (a) `made cursor doctor --json` correctly branches on `detail`, never fails `healthy` solely from `base_ref`; (b) a `made verify` run whose Review stage fails terminally still shows Test/Document/Lint at `pending` (or their real state) in the receipt, never absent; (c) `made verify complete --json` on a failure exits non-zero and prints a locatable `receipt_path`; (d) `made capabilities --json` lists `verify` and `cursor`.
- PR #22 is `state: closed`. PR #55 is either closed (superseded by the new fix-wave PR) or merged with corrected content - not merged with its original stale `.cursor/` commit.
- The user has run the follow-up canary prompt on real Cursor Cloud and confirmed (via a new PR comment) that findings 1, 4, and 9 no longer reproduce.
- Issue #43 has a final comment stating the honest close-out state and is closed, OR left open with an explicit "remaining: live canary matrix, lane-reuse in managed mode" note if the user prefers to keep it open as a living tracker for those deferred items.

### Must Have
- Every fix traced to a real, reproduced finding (from PR #55's comment or Metis's code trace) - no speculative hardening beyond what was actually found.
- `pending` reused as the "planned but unreached" status (no new status token, no schema vocabulary growth beyond what #39 already defined).
- The schema/version bump applied once, covering all of items 5's and 6's field additions together (not two separate bumps).
- PR #55 never merged with its stale `.cursor/` commit - regenerate-and-commit happens in the same commit as the skill.go fix, per the constraint below.

### Must NOT Have (guardrails, scope boundaries)
- Do NOT attempt the full ~15-scenario Cursor canary matrix from #43's own "Canary plan" section (a no-test repo, a second repo with a feature-map guide, ask-user decisions, HEAD-changes-mid-flow, model substitution, etc.). One real canary plus one small re-verification canary is this plan's ceiling; the rest is explicitly deferred, same precedent as #41/#42's Phase 4.
- Do NOT wire lane-based reuse (issue #33's reuse machinery) into `internal/managed`/`internal/verify`. Confirmed already an intentional, justified deferral from #41's own implementation; stays deferred.
- Do NOT build issue #27's release-please/signed-binary pipeline. The interim script is a stopgap, not a substitute.
- Do NOT loosen `internal/managed/externalreview.go`'s strict JSON decoding to auto-strip markdown code fences. This touches a security-sensitive external-input boundary; the canary agent already handled it caller-side without any Made change, and doing so isn't traceable to a concrete failure this plan needs to fix.
- Do NOT change `internal/managed/events.go`'s JSONL event stream shape as part of the stage-vanishing fix - only `StageResults`/the terminal manifest gain the previously-missing `pending` entries; no new `stage.completed` events are emitted for stages that never ran.
- Do NOT touch Consigliere or any external consumer's code (out of this repo). If the stage-reporting change could affect it, that's flagged as a risk, not remediated here.

## Verification Strategy
> ZERO HUMAN INTERVENTION beyond the two canary-prompt runs, which by definition require the user's real Cursor Cloud access - everything else is agent-executed.
- Test decision: **TDD** (RED-GREEN), matching this repo's existing, consistently-applied convention across every merged PR in this tracker so far.
- Framework: Go's standard `testing` package; `GIT_CONFIG_COUNT=1 GIT_CONFIG_KEY_0=commit.gpgsign GIT_CONFIG_VALUE_0=false` prefix required on every `go test` invocation that creates git fixtures (documented repo gotcha - avoids flaky SSH-signing failures).
- QA policy: every task below has agent-executed scenarios; two tasks (13 and the canary-prompt half of task 5's re-verification) require the user to run a Cursor Cloud prompt this plan produces as an artifact - that's the one unavoidable human-in-the-loop step, and it's explicit, not silent.
- Evidence: `evidence/task-{N}-{slug}.{ext}` per task, plus real command transcripts pasted into the task's completion note.

## Execution Strategy

### Parallel Execution Waves

**Wave 1** (foundation - independent packages, can run in parallel):
- Task 1: Split PR #55's branch (git-only, no Go code)
- Task 2: Close PR #22 + delete orphaned branch (administrative)
- Task 3: Fix the stage-vanishing bug at the source (`internal/managed/runner.go`) + regenerate golden fixtures + bump schema
- Task 4: Add `made_version`/`protocol_version`/`receipt_path`/`evidence_dir` to `verify.Receipt` (same version-bump policy as Task 3, coordinate on the exact new `schema_version` number)

**Wave 2** (depends on Task 1's branch existing; internal/cursor content changes are sequential within the same file):
- Task 5: Fix `internal/cursor/skill.go` content (doctor detail/status branching incl. `warn` case, `--base-ref` in doctor step, rewritten step 4 fallback for no-native-subagent harnesses)
- Task 6: Make `doctor.go`'s `base_ref` check informational-only (prerequisite for Task 5's `--base-ref` addition to be safe in shallow-clone Cloud VMs)
- Task 7: Regenerate `.cursor/` projections from the Task 5/6 fixes + add the committed-projection drift test (depends on 5 and 6)
- Task 8: Fix `requested_model` propagation in `internal/verify/prepare.go`
- Task 9: `cmd/made/verify.go` - fix `--json` exit codes for `run`/`complete` + surface `receipt_path`/`evidence_dir` in JSON output (depends on Task 4's new Receipt fields)
- Task 10: Add `verify`/`cursor` tokens to `capabilities --json` + assertion test + doc note

**Wave 3** (independent of Wave 2's content, but land together):
- Task 11: Interim pinned-install script (`scripts/install-cursor-cloud.sh`)
- Task 12: Land the fix branch: rebase Task 1's split branch onto `main` (which now has Tasks 3/4's merged changes), apply Wave 2's commits, open as the real replacement PR for #55, get CI green, merge

**Wave 4** (final - strictly after Task 12 merges):
- Task 13: Produce and hand the user the follow-up re-verification canary prompt; wait for their PR comment confirming findings 1/4/9 are fixed
- F1-F4: Final Verification Wave (plan compliance, code quality, full manual QA, scope fidelity)
- Task 14: Final DoD audit on #43 + close/update the issue

### Dependency Matrix (full, all tasks)

| Task | Depends On | Blocks |
|------|-----------|--------|
| 1 | - | 5, 6, 7, 12 |
| 2 | - | - |
| 3 | - | 12 (must be on main before Task 12's branch rebases) |
| 4 | - | 9, 12 |
| 5 | 1 | 7 |
| 6 | - | 5, 7 |
| 7 | 5, 6 | 12 |
| 8 | - | 12 |
| 9 | 4 | 12 |
| 10 | - | 12 |
| 11 | - | 12 (installer script referenced by skill's Installation section) |
| 12 | 1, 3, 4, 5, 6, 7, 8, 9, 10, 11 | 13 |
| 13 | 12 | 14 |
| F1-F4 | 12 | 14 |
| 14 | 13, F1-F4 | - |

## TODOs

- [x] 1. Split PR #55's branch: keep config/guide/docs, drop the stale `.cursor/` commit

  **What to do**: `git fetch origin` the PR #55 branch. Identify the exact commit(s) that added `.cursor/agents/made-reviewer.md` and `.cursor/skills/verify-with-made/SKILL.md` (`git log --follow -- .cursor/`). Interactively rebase (`git rebase -i`) to drop those commits entirely, keeping the commits that changed `.made.yaml` (adding `review.executors.cursor.model` and `review.guides`), added `.made/features/README.md`, and updated `README.md`. Force-push the cleaned branch to the same PR #55 branch name (`--force-with-lease`), so PR #55 now shows only the config/guide/doc diff with no `.cursor/` files.
  **Must NOT do**: Do not delete or rewrite the `.made/features/README.md` content itself - it's real, reviewed content from the canary run. Do not touch `.made.yaml`'s existing `review.required`/`ci`/`validation`/`commands` keys.

  **Parallelization**: Can Parallel: YES | Wave 1 | Blocks: 5, 6, 7, 12 | Blocked By: none

  **References**:
  - PR #55: `https://github.com/douglasjarquin/made/pull/55` - inspect via `gh pr diff 55` to see exactly which commit(s) touch `.cursor/`
  - Repo convention: this tracker's PRs (#50-#54) all used small atomic Conventional Commits, one logical change per commit - the rebase should preserve that granularity, not squash everything

  **Acceptance Criteria**:
  - [ ] `git diff origin/main...<branch>` (after the rebase) shows zero files under `.cursor/`
  - [ ] `git diff origin/main...<branch> -- .made.yaml .made/features/README.md README.md` shows exactly the canary's config/guide/doc changes, nothing else
  - [ ] `go build ./...` and `go test ./...` still pass on the cleaned branch (the repo should be unaffected by removing files that were never referenced by Go code)

  **QA Scenarios**:
  ```
  Scenario: Cleaned branch has no generated Cursor files
    Tool: bash
    Steps: git fetch origin feat/cursor-canary-55 (or whatever PR #55's branch is named); git diff origin/main...origin/feat/cursor-canary-55 --stat
    Expected: output contains .made.yaml, .made/features/README.md, README.md - no .cursor/ paths
    Evidence: evidence/task-1-split-pr55.txt

  Scenario: Build/test still green after the drop
    Tool: bash
    Steps: git checkout <cleaned branch>; go build ./... && GIT_CONFIG_COUNT=1 GIT_CONFIG_KEY_0=commit.gpgsign GIT_CONFIG_VALUE_0=false go test ./...
    Expected: both commands exit 0
    Evidence: evidence/task-1-build-test.txt
  ```

  **Commit**: NO (this task is a rebase/force-push of existing PR #55 commits, not a new commit)

- [x] 2. Close PR #22 as superseded; delete orphaned branch

  **What to do**: Confirm (already verified by Metis) that every file PR #22 introduced is present on `main` via #51-#54, except `.made.yml` (deliberately migrated to `.made.yaml`) and Consigliere-specific wording (deliberately generalized). Close PR #22 with a comment: "Superseded by #51, #52, #53, #54 - all of this PR's `internal/managed`/`internal/safegit` foundation, docs, and golden fixtures were imported and carried forward (see #51's PR body). Closing without merging." Check whether PR #22's source branch (e.g. `origin/codex/made-managed-validation-v1`) still exists on the remote; if so, delete it.
  **Must NOT do**: Do not force-close without the comment - the comment is the paper trail Metis's audit depended on.

  **Parallelization**: Can Parallel: YES | Wave 1 | Blocks: none | Blocked By: none

  **References**:
  - PR #22: `https://github.com/douglasjarquin/made/pull/22`
  - PR #51's body already documents the import lineage (`gh pr view 51 --full`)

  **Acceptance Criteria**:
  - [ ] `gh pr view 22 --json state` reports `CLOSED`
  - [ ] `git ls-remote origin | grep codex/made-managed-validation` (or whatever PR #22's exact branch was) returns nothing after cleanup

  **QA Scenarios**:
  ```
  Scenario: PR #22 closed with explanatory comment
    Tool: bash (gh CLI)
    Steps: gh pr close 22 --repo douglasjarquin/made --comment "<text above>"; gh pr view 22 --json state,comments
    Expected: state == "CLOSED", comments array contains the superseded-by text
    Evidence: evidence/task-2-close-pr22.json

  Scenario: Orphaned branch removed
    Tool: bash
    Steps: gh api repos/douglasjarquin/made/pulls/22 --jq .head.ref; git push origin --delete <that ref> (only if it still exists remotely)
    Expected: branch absent from a follow-up git ls-remote
    Evidence: evidence/task-2-branch-cleanup.txt
  ```

  **Commit**: NO (GitHub API/CLI actions, not repo commits)

- [x] 3. Fix the stage-vanishing bug at its source: `internal/managed/runner.go`

  **What to do**: In `Runner.Run`, when the loop returns early on the first stage producing a non-pass outcome, ensure every remaining stage from the plan that was going to `run` is still present in the returned `StageResults` at its already-initialized `pending` status (the vocabulary `pending`/`passed`/`failed`/`not_configured`/`disabled` already exists per issue #39's design - do not add a new status). Trace exactly where `StageResults` is initialized/populated today (per Metis: `runner.go:75-77` returns without appending remaining `planned` entries) and fix the initialization so ALL planned-to-run stages start at `pending` in the results map/slice up front, with only executed stages overwriting their own entry - so an early return naturally leaves the untouched ones at `pending` instead of missing. Bump `internal/managed`'s relevant schema/version marker (check `internal/managed/contract.go` and `internal/managed/evidence.go` for the exact versioned field(s) - likely `TerminalManifest`'s schema version) since this changes the shape of `stage_results`/`terminal.json` for real consumers. Regenerate every fixture in `internal/managed/testdata/*.jsonl` to match (failed-retryable.jsonl, failed-terminal.jsonl, infrastructure-error.jsonl, needs-decision.jsonl, passed.jsonl) and re-run `fixtures_test.go`'s validation.
  **Must NOT do**: Do not emit new `stage.completed` events for stages that never ran - the JSONL event stream itself is unchanged, only the final `StageResults`/terminal manifest gains the previously-missing `pending` entries. Do not touch `internal/verify/engine.go` in this task - it already just passes `runner.StageResults()` through, so fixing the source fixes both consumers for free.

  **Parallelization**: Can Parallel: YES | Wave 1 | Blocks: 12 | Blocked By: none

  **References**:
  - Bug location: `internal/managed/runner.go` (per Metis: lines ~59-61 has a comment claiming this merge already happens; lines ~75-77 is the actual early return that doesn't do it)
  - Consumers to keep correct: `internal/managed/run.go` (`TerminalManifest.stage_results`, ~line 174), `internal/verify/engine.go` (~lines 136, 153)
  - Versioned contract note: `docs/managed-validation-v1.md` documents "Managed V1 stops after the first `run` stage that produces a non-pass outcome... Later stages do not run." - this sentence needs a follow-up clause added: "...but remain visible in `stage_results` at `pending` rather than being omitted."
  - Golden fixtures: `internal/managed/testdata/*.jsonl`, validated by `internal/managed/fixtures_test.go`
  - Real-world reproduction: PR #55's canary comment shows a real receipt whose `stages` array contained only `review` when Test/Document/Lint were configured and never reached

  **Acceptance Criteria**:
  - [ ] A new unit test in `internal/managed/runner_test.go` (or the existing test file covering `Runner.Run`) constructs a plan with Review+Test+Lint all set to `run`, makes Review fail terminally, and asserts `StageResults()` contains Test and Lint both at `pending` (not absent)
  - [ ] `internal/managed/fixtures_test.go` passes against regenerated fixtures
  - [ ] `docs/managed-validation-v1.md`'s stage-stopping paragraph is updated in the same commit as the code change

  **QA Scenarios**:
  ```
  Scenario: Early terminal failure leaves later stages visible as pending
    Tool: bash (go test)
    Steps: GIT_CONFIG_COUNT=1 GIT_CONFIG_KEY_0=commit.gpgsign GIT_CONFIG_VALUE_0=false go test ./internal/managed/... -run TestRunner -v
    Expected: new test passes; asserts Test/Lint stage entries exist with Status=="pending" after Review fails
    Evidence: evidence/task-3-runner-pending-test.txt

  Scenario: Golden fixtures still validate after regeneration
    Tool: bash (go test)
    Steps: GIT_CONFIG_COUNT=1 GIT_CONFIG_KEY_0=commit.gpgsign GIT_CONFIG_VALUE_0=false go test ./internal/managed/... -run TestFixturesAreValid -v
    Expected: PASS, no drift between regenerated .jsonl files and what the code now produces
    Evidence: evidence/task-3-fixtures-valid.txt
  ```

  **Commit**: YES | Message: `fix(managed): keep unreached stages visible as pending instead of dropping them` | Files: `internal/managed/runner.go`, `internal/managed/testdata/*.jsonl`, `docs/managed-validation-v1.md`, new/updated `_test.go`

- [x] 4. Add `made_version`/`protocol_version`/`receipt_path`/`evidence_dir` to `verify.Receipt`

  **What to do**: In `internal/verify/receipt.go`, add four fields to the `Receipt` struct: `made_version` (string, from `internal/managed.MadeVersion`), `protocol_version` (int, matching whatever `internal/api.Version` or equivalent constant this repo already uses for its versioned contracts - check `internal/api` package for the existing constant name), `receipt_path` (string, the exact path this receipt was/will be written to - computable from `ReceiptsDir(StateRoot(root))` + input_sha, per `receipt.go` ~lines 90-92), and `evidence_dir` (string, the **per-invocation** evidence directory, not the evidence root - trace `EngineResult.EvidenceDir` in `internal/verify/engine.go` ~line 157 to confirm which one it currently holds and fix if it's the root). Bump `Receipt`'s `schema_version` constant by 1. Per this session's own no-backward-compat decision: do NOT add any migration/fallback for old cached receipts - `ReceiptStore.Get`'s existing hard rejection on schema mismatch (`receipt.go` ~lines 120-122) is correct as-is and needs no change; old receipts in `~/.cache/made/verify/` simply become unreadable, which is fine (disposable local cache).
  **Must NOT do**: Do not add a schema-migration path. Do not change `ReceiptStore.Get`'s rejection behavior.

  **Parallelization**: Can Parallel: YES | Wave 1 | Blocks: 9, 12 | Blocked By: none

  **References**:
  - `internal/verify/receipt.go` - `Receipt` struct definition, `schema_version` constant, `ReceiptsDir`/`StateRoot` helpers, `ReceiptStore.Get`'s rejection logic
  - `internal/verify/engine.go` ~line 157 - `EngineResult.EvidenceDir`, confirm root-vs-per-invocation semantics
  - `internal/managed/run.go` ~line 12 - `var MadeVersion = "dev"` (ldflags-settable)
  - Precedent: `internal/managed/evidence.go` ~line 108 - `TerminalManifest` already carries version identity; mirror its field naming

  **Acceptance Criteria**:
  - [ ] `Receipt` JSON output includes non-empty `made_version`, `protocol_version`, `receipt_path`, `evidence_dir` on every real run
  - [ ] `evidence_dir` points at the specific per-run evidence directory (verifiable: files actually exist under it after a real run), not the shared evidence root
  - [ ] `internal/verify/receipt_test.go` covers the new fields; a schema-mismatch test confirms old-schema receipts are still correctly rejected (unchanged behavior, just confirm it isn't broken by the bump)

  **QA Scenarios**:
  ```
  Scenario: Real receipt contains all four new fields with correct values
    Tool: bash (built binary against a real temp repo)
    Steps: build made; run `made verify run --json` in a temp git repo with a trivial passing config; parse the JSON; assert made_version/protocol_version/receipt_path/evidence_dir are all present and non-empty; ls -la "$evidence_dir" to confirm it's a real, populated directory
    Expected: all four fields present; evidence_dir directory exists and is non-empty
    Evidence: evidence/task-4-receipt-fields.json

  Scenario: Old-schema receipt correctly rejected, not silently migrated
    Tool: go test
    Steps: GIT_CONFIG_COUNT=1 GIT_CONFIG_KEY_0=commit.gpgsign GIT_CONFIG_VALUE_0=false go test ./internal/verify/... -run TestReceipt -v
    Expected: a test writing a receipt file with the OLD schema_version and reading it back via ReceiptStore.Get fails with a clear "schema mismatch" error, not a panic or silent partial read
    Evidence: evidence/task-4-schema-rejection.txt
  ```

  **Commit**: YES | Message: `feat(verify): add version and location identity to receipts` | Files: `internal/verify/receipt.go`, `internal/verify/engine.go`, `internal/verify/receipt_test.go`

- [x] 5. Fix `internal/cursor/skill.go` content: doctor branching, `--base-ref`, step 4 fallback

  **What to do**: Three changes to `SkillMarkdown()`'s generated body text (and its `_test.go` assertions):
  (a) Step 4's condition must key off the `cursor_executor` check's **`detail`** field, not `status` (`doctor.go` always sets `status` to ok/warn/skipped; the literal `configured`/`not_configured` strings live in `detail`). Add a third branch for `status == "warn"` (meaning `review.required: true` but no model configured, per `doctor.go` ~lines 79-80): the skill should tell the agent to stop and report the doctor warning rather than falling through to either the Cursor-review or no-review path silently.
  (b) The doctor invocation in step 2 must pass `--base-ref <trusted-ref>` (the same ref the skill already tells the agent to pass to `made verify prepare`), so the `base_ref` check actually runs instead of always showing `skipped`. This is only safe because Task 6 makes that check non-fatal to `healthy` first - sequence this task after Task 6 lands, or land them in the same commit.
  (c) Step 4's "invoke `made-reviewer` with the exact contents of `request_path` as its only input - do not summarize, paraphrase, or add anything" instruction must be rewritten to handle harnesses with no native "run this custom `.cursor/agents/*.md` subagent file" primitive: add explicit fallback wording along the lines of "If your harness has no built-in mechanism to invoke a named custom subagent file, read `made-reviewer.md`'s frontmatter and body yourself, and launch a fresh subagent whose entire system prompt is that body verbatim, then give it the prepared request as its input" - this matches exactly what the canary agent already improvised successfully on Cursor Cloud's `Task` tool.
  **Must NOT do**: Do not change the reviewer file's own content (`internal/cursor/reviewer.go`) in this task - only the skill's instructions to the calling agent. Do not remove the "do not summarize/paraphrase" constraint for the request itself (that still applies) - only add the fallback for how to launch the subagent, not license paraphrasing the request content.

  **Parallelization**: Can Parallel: NO (same file as Task 6/7) | Wave 2 | Blocks: 7 | Blocked By: 1, 6

  **References**:
  - `internal/cursor/skill.go` - `skillBody` constant, steps 2 and 4
  - `internal/cursor/doctor.go` - `cursor_executor` check emits `{Status: StatusOK, Detail: "configured"}` / `{Status: StatusOK, Detail: "not_configured"}` / (when required but unconfigured) a `warn` status
  - Real bug evidence: PR #55's canary comment, "Skill step 4 vs doctor schema (the blocking finding)" section, with the exact live JSON showing `status: "ok", detail: "configured"`
  - Real fallback evidence: PR #55's canary comment, friction item 4 ("`.cursor/agents/made-reviewer.md` is not a first-class Cloud subagent... I launched `generalPurpose`... and had to prepend the made-reviewer standing instructions")

  **Acceptance Criteria**:
  - [ ] `SkillMarkdown()`'s output contains the substring `` `detail` `` in the step 4 branching instruction and does NOT contain the literal phrase `status \`configured\`` (mirrors Metis's suggested QA assertion)
  - [ ] Step 2's doctor command in the generated text includes `--base-ref`
  - [ ] Step 4 includes the no-native-subagent fallback wording
  - [ ] `internal/cursor/skill_test.go`'s existing forbidden-phrase test (checking for "start the Made daemon", "made gate init", etc.) still passes unchanged

  **QA Scenarios**:
  ```
  Scenario: Generated skill branches on detail, not status
    Tool: go test
    Steps: GIT_CONFIG_COUNT=1 GIT_CONFIG_KEY_0=commit.gpgsign GIT_CONFIG_VALUE_0=false go test ./internal/cursor/... -run TestSkillMarkdown -v
    Expected: new/updated assertions pass - body contains "detail" branching language, does not contain the old "status `configured`" phrasing
    Evidence: evidence/task-5-skill-content.txt

  Scenario: Real doctor output correctly matches the new skill instructions
    Tool: bash (built binary)
    Steps: build made; run `made cursor doctor --json` against this repo (which has review.executors.cursor.model set via Task 1's cleaned config); confirm the printed JSON's cursor_executor.detail == "configured" and manually trace that the NEW skill text's branching condition would correctly select the Cursor-review path for this exact output
    Expected: match confirmed by inspection of real JSON against new skill text
    Evidence: evidence/task-5-doctor-match.json
  ```

  **Commit**: YES (combine with Task 6 in the same commit since Task 5(b) depends on Task 6) | Message: `fix(cursor): key skill's review-branch on doctor detail, not status; add base-ref; document subagent-invocation fallback` | Files: `internal/cursor/skill.go`, `internal/cursor/skill_test.go`

- [x] 6. Make `doctor.go`'s `base_ref` check informational-only

  **What to do**: In `internal/cursor/doctor.go`, find the `base_ref` check's status-setting logic (per Metis: currently the check can produce `StatusFail` when the ref can't be resolved locally, which flips `Doctor()`'s overall `healthy` to `false`). Change it so an unresolvable `base_ref` always caps at `StatusWarn` (or whatever this codebase's non-fatal status constant is named), never `StatusFail` - matching the fact that a Cloud VM with a shallow/limited clone may not have every ref available locally, and that's a real, survivable condition the skill should proceed past, not abort on.
  **Must NOT do**: Do not make the check silently pass with no signal at all - it should still show its real state (warn/skipped), just never gate `healthy`.

  **Parallelization**: Can Parallel: NO (small, but must land before/with Task 5) | Wave 2 | Blocks: 5, 7 | Blocked By: none

  **References**:
  - `internal/cursor/doctor.go` - `checkWritable`, the `base_ref` check function, and `Doctor()`'s `healthy` aggregation logic (per Metis: `doctor.go` ~lines 46-48 only clears `healthy` on `StatusFail`; ~lines 135-136 is where `base_ref` currently can reach `StatusFail`)

  **Acceptance Criteria**:
  - [ ] A test constructs a repo where the given `--base-ref` doesn't exist locally, runs `Doctor()`, and asserts `healthy == true` with the `base_ref` check at `warn` (not `fail`)
  - [ ] A test with a resolvable `--base-ref` still shows the check succeeding as before (no regression)

  **QA Scenarios**:
  ```
  Scenario: Unresolvable base-ref never fails healthy
    Tool: go test
    Steps: GIT_CONFIG_COUNT=1 GIT_CONFIG_KEY_0=commit.gpgsign GIT_CONFIG_VALUE_0=false go test ./internal/cursor/... -run TestDoctor -v
    Expected: new case passes - healthy:true even with an unresolvable base-ref
    Evidence: evidence/task-6-baseref-warn.txt
  ```

  **Commit**: (bundled with Task 5's commit, see above)

- [x] 7. Regenerate `.cursor/` projections + add committed-file drift test

  **What to do**: On the branch from Task 1 (now with Tasks 5+6's skill.go/doctor.go fixes applied), run `made cursor sync` (build the binary first) to regenerate `.cursor/agents/made-reviewer.md` and `.cursor/skills/verify-with-made/SKILL.md` from the current `.made.yaml`, and commit them fresh (these are the files PR #55's stale commit was dropped for in Task 1). Then add a test - reusing `internal/cursor/check.go`'s existing `Check(root, cfg)` drift-detection function (the same one `made cursor check` uses), not inventing a new `VerifyCommitted`-style function - that loads this repo's real `.made.yaml`, calls `Check` against the actual committed `.cursor/` files, and asserts zero drift findings. Name it analogously to `internal/skill`'s `TestCommittedSkillFileMatchesGenerator`, e.g. `TestCommittedCursorProjectionsMatchGenerator`, in a new `internal/cursor/committed_test.go` (or add to an existing test file if more natural).
  **Must NOT do**: Do not hand-edit the generated `.cursor/` files - they must come only from running `made cursor sync`. Do not skip this test if `.made.yaml`'s cursor config is ever later removed - the test should correctly assert "no `.cursor/agents/made-reviewer.md` expected" in that case too (mirroring `sync`'s own removal behavior), not just fail confusingly.

  **Parallelization**: Can Parallel: NO (depends on 5, 6) | Wave 2 | Blocks: 12 | Blocked By: 5, 6

  **References**:
  - Precedent: `internal/skill/skill_test.go`'s `TestCommittedSkillFileMatchesGenerator` calling `skill.VerifyCommitted(path)`
  - Function to reuse: `internal/cursor/check.go`'s `Check(root, cfg)` (used by `made cursor check` CLI, per `cmd/made/cursor.go`)
  - Removal behavior precedent: `internal/cursor/sync.go` removes the reviewer file when the model is unset - the drift test should be consistent with this, not contradict it

  **Acceptance Criteria**:
  - [ ] `.cursor/agents/made-reviewer.md` and `.cursor/skills/verify-with-made/SKILL.md` are committed, byte-identical to running `made cursor sync` fresh
  - [ ] `TestCommittedCursorProjectionsMatchGenerator` exists and passes
  - [ ] `made cursor check` (the real CLI command) reports no drift when run against this repo after the commit
  - [ ] A contributor note is added to `AGENTS.md`: editing `internal/cursor/skill.go`/`reviewer.go` or `.made.yaml`'s cursor/guide config requires re-running `made cursor sync` and committing the result, or CI's drift test fails

  **QA Scenarios**:
  ```
  Scenario: Committed projections match fresh generator output
    Tool: go test
    Steps: GIT_CONFIG_COUNT=1 GIT_CONFIG_KEY_0=commit.gpgsign GIT_CONFIG_VALUE_0=false go test ./internal/cursor/... -run TestCommittedCursorProjectionsMatchGenerator -v
    Expected: PASS
    Evidence: evidence/task-7-drift-test.txt

  Scenario: Real CLI confirms no drift
    Tool: bash (built binary)
    Steps: go build -o /tmp/made-bin ./cmd/made && /tmp/made-bin cursor check --repo .
    Expected: exits 0, reports current/no drift
    Evidence: evidence/task-7-cursor-check.txt

  Scenario: Drift IS detected when generator output would differ
    Tool: go test
    Steps: temporarily mutate a copy of the committed SKILL.md in a test tempdir (not the real repo file) and run Check against it
    Expected: Check reports a drift finding - proves the test isn't vacuously passing
    Evidence: evidence/task-7-drift-detected.txt
  ```

  **Commit**: YES | Message: `feat(cursor): regenerate projections after skill.go fix; add committed-file drift test` | Files: `.cursor/agents/made-reviewer.md`, `.cursor/skills/verify-with-made/SKILL.md`, `internal/cursor/committed_test.go`, `AGENTS.md`

- [x] 8. Fix `requested_model` propagation in `internal/verify/prepare.go`

  **What to do**: `Prepare()` currently only sets the request's `requested_model` from an explicit, undocumented `--requested-model` CLI flag - the real canary request had no `requested_model` key at all despite `.made.yaml` configuring `review.executors.cursor.model`. Fix: when `--executor cursor` is given and `--requested-model` is NOT explicitly passed, default `requested_model` from the resolved config's `review.executors.cursor.model` (note per Metis: `ResolvedContext` in `internal/verify/repo.go` currently only retains `ConfigBytes`/`Guides`, not the parsed `config.Config` - extend it to also carry the parsed config, or re-parse `ConfigBytes` at the point `requested_model` is populated, whichever is the smaller diff). `--requested-model` still overrides when explicitly given. Since `config.ReviewExecutors` today has a single concrete `Cursor` field (not a map), the executor-name-to-config-field mapping is a small hardcoded switch on `--executor`'s value - keep it that way, don't generalize to a map for one executor.
  **Must NOT do**: Do not change `internal/verify/complete.go`'s existing behavior of preferring the reviewer result's own echoed `requested_model` when validating the result (that's correct, provenance-only design, confirmed intentional) - this task is only about what `prepare` puts INTO the outgoing request.

  **Parallelization**: Can Parallel: YES | Wave 2 | Blocks: 12 | Blocked By: none

  **References**:
  - `internal/verify/prepare.go` (per Metis: ~line 45, `p.RequestedModel` used directly with no config fallback)
  - `internal/verify/repo.go` (per Metis: `ResolveContext` ~line 78 parses config; `ResolvedContext` ~lines 25-34 only keeps `ConfigBytes`/`Guides` today)
  - `internal/config/config.go` - `Config.Review.Executors.Cursor.Model`
  - Real bug evidence: PR #55 canary comment, "did not contain `requested_model` at all (`"requested_model" in request` -> `False`)"

  **Acceptance Criteria**:
  - [ ] `made verify prepare --executor cursor` with `review.executors.cursor.model` configured and no `--requested-model` flag produces a request JSON containing `requested_model` equal to the config value
  - [ ] `--requested-model` explicitly passed still overrides the config value
  - [ ] `--executor cursor` with NO `review.executors.cursor.model` configured produces a request with no `requested_model` key (unchanged from today - nothing to default from)

  **QA Scenarios**:
  ```
  Scenario: Config value flows into the request by default
    Tool: bash (built binary, real temp repo)
    Steps: create a temp repo with .made.yaml setting review.executors.cursor.model: "claude-opus-5[effort=high]"; commit; run `made verify prepare --executor cursor --base-ref <base> --json`; cat the request file
    Expected: request JSON's requested_model == "claude-opus-5[effort=high]"
    Evidence: evidence/task-8-model-default.json

  Scenario: Explicit flag overrides config
    Tool: bash
    Steps: same repo, run `made verify prepare --executor cursor --requested-model "gpt-5" --base-ref <base> --json`; cat the request file
    Expected: requested_model == "gpt-5", not the config's value
    Evidence: evidence/task-8-model-override.json
  ```

  **Commit**: YES | Message: `fix(verify): default requested_model from configured review executor` | Files: `internal/verify/prepare.go`, `internal/verify/repo.go`, `internal/verify/prepare_test.go`

- [x] 9. Fix `--json` exit codes and surface receipt location in `cmd/made/verify.go`

  **What to do**: Two related fixes in `cmd/made/verify.go`'s `emitJSON` path (used by both `made verify run --json` and `made verify complete --json` - per Metis, the bug affects both, not just `complete` as originally reported):
  (a) `emitJSON` currently always returns exit code 0 regardless of outcome. Fix it to return the same exit code the human-output path already uses (`Outcome.ExitCode()`: 0 passed / 1 infrastructure_error / 2 usage-error / 3 needs_decision / 4 failed_retryable / 5 failed_terminal / 130 canceled) while STILL printing the JSON body to stdout (a caller must be able to parse JSON on any exit code, including failure - this is different from a CLI usage error, which prints no JSON at all and exits 2). `status`/`receipt` subcommands are read-only inspection and should keep exiting 0 regardless of the receipt's own outcome (only `run`/`complete` change).
  (b) Once Task 4 lands `receipt_path`/`evidence_dir` on `Receipt`, make sure `run`/`complete`'s JSON output actually includes them (if `emitJSON` marshals `Receipt` directly, this may already be automatic - verify, don't assume).
  **Must NOT do**: Do not change `verify status --json` or `verify receipt --json`'s exit-code behavior. Do not swallow the JSON body on failure - both the JSON and the correct exit code must be present together.

  **Parallelization**: Can Parallel: NO (depends on Task 4's new Receipt fields) | Wave 2 | Blocks: 12 | Blocked By: 4

  **References**:
  - `cmd/made/verify.go` - `emitJSON` (per Metis: ~lines 289-296), the `run` command (~line 68 also affected), `Outcome.ExitCode()` (already correctly used by the human-output path)
  - Real bug evidence: PR #55 canary comment, "`--json` process exit was `0`; human (non-`--json`) complete exit was `5`"

  **Acceptance Criteria**:
  - [ ] `made verify complete --json` on a `failed_terminal` outcome exits 5 and still prints valid JSON to stdout
  - [ ] `made verify run --json` on the same class of failure exits with the matching non-zero code
  - [ ] `made verify status --json` / `made verify receipt <sha> --json` continue to exit 0 when they successfully report on (even a failed) receipt - only `run`/`complete` change
  - [ ] The JSON body for `run`/`complete` includes `receipt_path` and `evidence_dir` (from Task 4)

  **QA Scenarios**:
  ```
  Scenario: Failing complete --json exits non-zero with valid JSON
    Tool: bash (built binary, real temp repo)
    Steps: set up a temp repo where Review is configured to fail (e.g. external review result with a blocking finding); run `made verify complete --request ... --review-result ... --json`; capture $?; pipe stdout through `jq .`
    Expected: exit code == 5 (or the correct ExitCode() value for the outcome produced); jq parses the output without error
    Evidence: evidence/task-9-json-exitcode.txt

  Scenario: status/receipt unaffected
    Tool: bash
    Steps: made verify status --head --json against a repo with a failed receipt; echo $?
    Expected: exit 0 (inspection command, not gated by receipt outcome)
    Evidence: evidence/task-9-status-unaffected.txt
  ```

  **Commit**: YES | Message: `fix(cmd/verify): make --json exit codes match human-output outcome codes` | Files: `cmd/made/verify.go`, `cmd/made/verify_test.go`

- [x] 10. Add `verify`/`cursor` tokens to `capabilities --json`

  **What to do**: In `cmd/made/runcommands.go`'s capabilities builder (per Metis: ~line 43), additively add `verify` and `cursor` as top-level command tokens (matching the existing single-word style already used for `doctor`/`gate`, not a per-subcommand style like `verify.prepare`). Do NOT bump `capabilities`'s own `schema_version` - this mirrors how `validate.managed.v1` was added additively without a version bump in #39. Add a new assertion in the existing capabilities contract test file (`cmd/made/contracts_red_test.go` or `remediation_contract_test.go` - per Metis, today's tests only assert presence of the OLD tokens, so this addition is currently unverified) that explicitly checks for `verify` and `cursor` in the commands list. Add one line to `docs/managed-validation-v1.md` (or wherever this repo documents its capabilities contract most naturally) noting these two additions.
  **Must NOT do**: Do not restructure the existing capabilities schema or rename any existing token.

  **Parallelization**: Can Parallel: YES | Wave 2 | Blocks: 12 | Blocked By: none

  **References**:
  - `cmd/made/runcommands.go` - capabilities command-list builder (~line 43 per Metis)
  - Existing precedent: `validate.managed.v1` addition from #39/PR #51
  - Existing tests to extend: `cmd/made/contracts_red_test.go` (~line 33), `cmd/made/remediation_contract_test.go` (~line 33)
  - Real gap evidence: PR #55 canary comment, "its `commands` list is still only `run.submit`/`run.status`/`run.list`/`run.cancel`/`review.decide`/`doctor`/`validate.managed.v1` - no `verify` or `cursor`"

  **Acceptance Criteria**:
  - [ ] `made capabilities --json` output's commands list includes both `verify` and `cursor`
  - [ ] A new/updated test asserts their exact presence (not just "capabilities returns something")
  - [ ] Existing capabilities consumers (any other test asserting the exact full list) are updated, not left to silently fail

  **QA Scenarios**:
  ```
  Scenario: Capabilities lists the new commands
    Tool: bash (built binary)
    Steps: made capabilities --json | jq '.commands'
    Expected: array/object includes "verify" and "cursor"
    Evidence: evidence/task-10-capabilities.json

  Scenario: Contract test locks the exact tokens
    Tool: go test
    Steps: GIT_CONFIG_COUNT=1 GIT_CONFIG_KEY_0=commit.gpgsign GIT_CONFIG_VALUE_0=false go test ./cmd/made/... -run TestCapabilities -v
    Expected: PASS, explicitly checks for "verify" and "cursor" tokens
    Evidence: evidence/task-10-contract-test.txt
  ```

  **Commit**: YES | Message: `feat(cmd): advertise verify and cursor commands in capabilities` | Files: `cmd/made/runcommands.go`, `cmd/made/contracts_red_test.go`, `docs/managed-validation-v1.md`

- [x] 11. Interim pinned-install script for Cursor Cloud

  **What to do**: Add `scripts/install-cursor-cloud.sh`: a POSIX shell script that (a) takes a pinned commit SHA (default: the SHA of `HEAD` at build/CI time, or accept an env var / positional arg override), (b) clones or uses a local checkout of `douglasjarquin/made` at that exact SHA, (c) builds the `made` binary with `go build -ldflags "-X github.com/douglasjarquin/made/internal/managed.MadeVersion=<sha>" -o <install-dir>/made ./cmd/made` so receipts stop reporting `version=dev` and instead show the real pinned SHA, (d) also installs `golangci-lint` pinned to the same version this repo's CI workflow uses (`v2.11.2`, per `.github/workflows/ci.yml`), since this repo's own `.made.yaml` configures `commands.lint: golangci-lint run ./...` and the canary's Cloud VM had neither tool. Update the generated skill's "Installation" section (`internal/cursor/skill.go`) to name this script as the documented interim path, replacing the current vague "install a pinned development build" prose - regenerate `.cursor/skills/verify-with-made/SKILL.md` again if this is added after Task 7 (or fold into Task 7's regeneration if sequenced together).
  **Must NOT do**: Do not attempt release-please, code-signing, or checksummed release artifacts - that is issue #27's scope, not this script's.

  **Parallelization**: Can Parallel: YES | Wave 3 | Blocks: 12 | Blocked By: none (but its skill.go doc reference should land no later than Task 7/12)

  **References**:
  - `internal/managed/run.go` ~line 12 - `var MadeVersion = "dev"`, ldflags-settable
  - `.github/workflows/ci.yml` - confirm exact `golangci-lint` version pinned there (v2.11.2 per earlier CI logs in this session)
  - `internal/cursor/skill.go` - "## Installation" section text
  - Real gap evidence: PR #55 canary friction #1 ("Made was not on PATH in this Cloud environment") and #9 ("`golangci-lint` was not on PATH... I installed golangci-lint v2.5.0" - note the canary agent used a DIFFERENT version than CI's, which is itself a latent inconsistency this script fixes)

  **Acceptance Criteria**:
  - [ ] Running the script in a clean environment produces a `made` binary whose `made capabilities --json` (or a receipt from a real run) shows the pinned SHA, not `dev`
  - [ ] The script also leaves a working `golangci-lint` matching CI's exact pinned version on PATH
  - [ ] The regenerated skill's Installation section references the script by name/path

  **QA Scenarios**:
  ```
  Scenario: Script installs a version-identified made binary
    Tool: bash
    Steps: run scripts/install-cursor-cloud.sh in a scratch directory/container; run the resulting made's `made verify run --json` in a trivial repo; inspect made_version field (from Task 4) in the resulting receipt
    Expected: made_version equals the pinned SHA passed to the script, not "dev"
    Evidence: evidence/task-11-pinned-version.json

  Scenario: golangci-lint installed and matches CI's version
    Tool: bash
    Steps: after running the script, `golangci-lint version`
    Expected: version string matches .github/workflows/ci.yml's pinned version exactly
    Evidence: evidence/task-11-lint-version.txt
  ```

  **Commit**: YES | Message: `feat(cursor): add interim pinned-install script for Cloud environments` | Files: `scripts/install-cursor-cloud.sh`, `internal/cursor/skill.go` (Installation section text)

- [x] 12. Land the fix branch: rebase, apply Wave 2/3 commits, open PR, merge

  **What to do**: Fetch latest `main` (now containing Tasks 3 and 4's merged Wave 1 commits, assuming Wave 1 tasks are landed as their own small PRs first - or, if this plan's executor prefers, land Wave 1 tasks 3+4 as commits on THIS SAME branch instead of separate PRs, whichever keeps `main` green at every intermediate step per this repo's established convention from the #38-#42 chain). Rebase Task 1's cleaned PR #55 branch onto current `main`. Apply Tasks 5, 6, 7, 8, 9, 10, 11's commits on top, in dependency order (5+6 together, then 7, then 8/9/10/11 which are largely independent of each other). Run the full local verification suite. Push and open a new PR (or reuse PR #55's branch/number if GitHub allows updating it in place - either is fine, just don't silently lose the PR #55 history/comments that document the original canary). Wait for `build-test-lint` CI to pass (the `dogfood-required` attestation check is expected to fail for the same pre-existing local sandbox-exec/codex reason documented on every PR in this tracker - not a blocker, per the user's own earlier decision to merge past it). Merge (squash, matching this repo's established convention).
  **Must NOT do**: Do not merge with `.cursor/` files that don't match Task 7's freshly-generated ones. Do not skip the full race/shuffle test run before merging - every prior PR in this tracker did this and it caught real issues.

  **Parallelization**: Can Parallel: NO (integration point) | Wave 3 | Blocks: 13 | Blocked By: 1, 3, 4, 5, 6, 7, 8, 9, 10, 11

  **References**:
  - This tracker's own established merge pattern: PRs #50-#54, each rebased onto the previous merge with `git rebase --onto`, verified with full build+test+race before force-push, squash-merged with `--delete-branch`
  - `GIT_CONFIG_COUNT=1 GIT_CONFIG_KEY_0=commit.gpgsign GIT_CONFIG_VALUE_0=false` prefix for all `go test` runs

  **Acceptance Criteria**:
  - [ ] `go build ./...`, `go vet ./...`, `go test ./...`, `go test -race -shuffle=on -count=1 ./...` all pass on the final branch before merge
  - [ ] `build-test-lint` CI check passes on the PR
  - [ ] PR merges cleanly into `main`; local `main` fast-forwards and re-verifies green after merge

  **QA Scenarios**:
  ```
  Scenario: Full suite green pre-merge
    Tool: bash
    Steps: on the final branch: go build ./... && go vet ./... && GIT_CONFIG_COUNT=1 GIT_CONFIG_KEY_0=commit.gpgsign GIT_CONFIG_VALUE_0=false go test ./... && GIT_CONFIG_COUNT=1 GIT_CONFIG_KEY_0=commit.gpgsign GIT_CONFIG_VALUE_0=false go test -race -shuffle=on -count=1 ./...
    Expected: all four commands exit 0
    Evidence: evidence/task-12-full-suite.txt

  Scenario: Post-merge main is green
    Tool: bash
    Steps: git checkout main; git pull --ff-only; go build ./... && go test ./...
    Expected: both exit 0
    Evidence: evidence/task-12-postmerge.txt
  ```

  **Commit**: N/A (this task is the merge itself, not a new commit)

- [x] 13. Follow-up live-canary re-verification prompt

  **What to do**: Write a short, focused Cursor Cloud prompt (much smaller than the original canary prompt) that specifically re-exercises findings 1, 4, and 9 - the three that are only provable in that real environment: (a) confirm `made cursor doctor --json`'s `cursor_executor` check and the skill's new step 4 branching now correctly select the external-review path on this repo's real config; (b) confirm `made cursor doctor --json --base-ref origin/main` no longer shows `base_ref: skipped` in a normal clone, and confirm a deliberately-unresolvable ref still reports `healthy: true`; (c) confirm the rewritten step 4 fallback wording actually results in a schema-valid external review result when the calling harness (Cursor Cloud's Task tool) again has no native custom-subagent invocation. Hand this prompt to the user (as this task's actual deliverable - present it in chat, do not just describe it). Wait for them to run it and return the resulting PR comment or summary.
  **Must NOT do**: Do not mark this task complete until the user has actually run it and reported back - "I wrote a good prompt" is not sufcient evidence, matching this whole plan's own standard of tracing every claim to a real, executed check.

  **Parallelization**: Can Parallel: NO | Wave 4 | Blocks: 14 | Blocked By: 12

  **References**:
  - Original canary prompt (given to the user earlier in this session) and its resulting PR #55 comment, as the template/format to follow
  - Findings 1, 4, 9 as fixed in Tasks 5, 6, and 5(c) respectively

  **Acceptance Criteria**:
  - [ ] Prompt text is presented to the user, not merely summarized
  - [ ] User has run it on real Cursor Cloud and returned a result (PR comment, transcript, or direct report)
  - [ ] The result confirms findings 1, 4, and 9 no longer reproduce, OR surfaces a new, real problem that must be triaged before closing #43 (in which case, treat as a new finding, not silently ignored)

  **QA Scenarios**:
  ```
  Scenario: Re-verification confirms the fixes
    Tool: manual (Cursor Cloud, human-in-the-loop by design)
    Steps: user runs the prompt against this repo in Cursor Cloud; posts the resulting comment/transcript
    Expected: doctor/skill branching now correctly selects external review; base_ref check behaves as fixed; step 4's fallback produces a valid result
    Evidence: linked PR comment or pasted transcript, referenced in this task's completion note

  Scenario: Re-verification finds a new issue
    Tool: manual
    Steps: same as above
    Expected: if something new is found, it is written up and either fixed in a follow-up task or explicitly deferred with reasoning - never silently dropped
    Evidence: same as above, plus a note in the plan's final summary
  ```

  **Commit**: NO

## Final Verification Wave (MANDATORY - after ALL implementation tasks)
> ALL must APPROVE. Present consolidated results to user and get explicit "okay" before completing.

- [x] F1. Plan Compliance Audit
  Verify every task above (1-13) is marked complete with its acceptance criteria checked and QA evidence present. Confirm no task was silently skipped or downgraded from what's specified here.

- [x] F2. Code Quality Review
  Review the full diff introduced by Tasks 3-11 for adherence to this repo's established conventions (no comments except non-obvious "why", smallest correct change, Conventional Commits, no drive-by refactors). Confirm the schema-version bump from Tasks 3/4 is applied exactly once and consistently, not as two separate uncoordinated bumps.

- [x] F3. Real Manual QA
  Re-run every QA Scenario above against the final merged `main`, not just against intermediate branches. Build the real binary fresh from `main` and re-execute the CLI-facing scenarios (Tasks 5, 7, 8, 9, 10, 11) one more time end to end.

- [x] F4. Scope Fidelity Check
  Confirm nothing in "Must NOT Have" was violated: no full canary matrix attempted beyond Tasks 13's targeted re-check, no lane-reuse wiring added to managed/verify, no release-pipeline work, no loosened external-review JSON parsing, no new JSONL event types.

- [x] 14. Final DoD audit on #43 + close/update the issue

  **What to do**: Re-read issue #43's full Definition of Done checklist. For each item, mark it against real, verified state (not assumption): items closed by #38-#42 (already true), items closed by this plan's Tasks 1-13, and the two categories explicitly deferred (the full ~15-scenario canary matrix across two repos; lane-based reuse wiring inside `internal/managed`/`internal/verify`) - state these as deliberate, precedented deferrals (matching #41/#42's own Phase 4 deferrals), not gaps nobody noticed. Post this audit as a comment on #43. Ask the user whether to close #43 now (with the deferred items tracked as new, smaller follow-up issues if they want continued visibility) or leave it open as a living tracker for exactly those two deferred categories - this is their call, not a silent default, since it changes what "done" means for the whole tracker.
  **Must NOT do**: Do not close #43 claiming full completion when the canary matrix and lane-reuse items are known, real, unaddressed gaps - that would misrepresent the tracker's actual state, which is exactly the kind of "truthful coverage" failure this whole plan exists to fix elsewhere in the codebase.

  **Parallelization**: Can Parallel: NO | Wave 4 | Blocks: none | Blocked By: 13, F1, F2, F3, F4

  **References**: Issue #43's own DoD checklist and Canary plan sections (already fully read earlier in this session)

  **Acceptance Criteria**:
  - [ ] A comment is posted on #43 auditing every DoD line item against real, verified evidence
  - [ ] Deferred items are named explicitly, with a stated reason and precedent
  - [ ] The user has been asked, and has answered, whether to close #43 now or keep it open for the deferred items

  **QA Scenarios**:
  ```
  Scenario: Audit comment is accurate and complete
    Tool: manual review + gh CLI
    Steps: gh pr view/issue view cross-checks for every DoD line; post via gh issue comment 43
    Expected: comment posted, covers every DoD checkbox, no item silently omitted
    Evidence: linked comment URL
  ```

  **Commit**: NO

## Commit Strategy
- One PR per Wave-1 task (1, 2, 3, 4) where feasible, since they're independent and touch different files/systems - OR bundle 3+4 into one small PR (`internal/managed`/`internal/verify`, same version-bump concern) if that's cleaner for the executor; either is acceptable as long as `main` stays green after each merge.
- Tasks 5-11 land together as one PR (the rebuilt replacement for PR #55), since they're sequentially dependent on the same branch and conceptually one fix wave.
- Atomic Conventional Commits within that PR, one per task as specified above (5+6 combined, 7, 8, 9, 10, 11 separate).
- Never add an agent name as commit co-author, matching this session's established convention.
- Squash-merge each PR, matching this tracker's established convention (#50-#54).

## Success Criteria
- Every one of PR #55's 8 confirmed real findings has a corresponding code fix, test, and QA evidence trail in this plan.
- `main` passes `go build`, `go vet`, `go test`, and `go test -race -shuffle=on -count=1` at every merge point, with zero regressions introduced.
- PR #22 is closed as superseded; PR #55's stale content is never merged as-is.
- The user has run a real follow-up Cursor Cloud canary confirming findings 1, 4, and 9 are actually fixed in the live environment, not just in unit tests.
- Issue #43 carries an honest, evidence-backed final audit distinguishing "genuinely closed" from "deliberately deferred," and the user has explicitly decided whether to close it or keep it open for the deferred items.

