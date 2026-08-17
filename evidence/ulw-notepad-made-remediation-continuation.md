# Ultrawork Notepad — Continue Made remediation from exact base

Started: 2026-08-17T00:00:00-04:00

## Plan (exhaustively detailed)

1. Prove the isolated worktree, exact base, source and prior-worktree custody, installed Made and required tools, live daemon state, Herdr lab isolation, and the canonical continuation checklist.
2. Map every still-valid continuation hypothesis to a Made-owned contract, test seam, strict external fake, RED evidence, GREEN implementation, real-surface QA scenario, cleanup receipt, and phase-local evidence artifact.
3. Implement external-tool contracts for GitHub checks and Codex review, explicitly reject unsupported Claude behavior where required, and verify each focused contract.
4. Implement durability and lifecycle fixes as vertical RED-to-GREEN slices, preserving durable run identity, stage, decisions, evidence, restart, configuration, and reviewer containment.
5. Update the canonical Made plan with a linked continuation section and phase-scoped evidence, run Made-only compatibility/build/test/vet/lint validation, perform manual QA through the allowed surfaces, and reconcile custody.
6. Commit verified increments from the exact base, render any bossless decision record if present, push only the task branch, open the direct PR with gh-axi, and report exact custody.

## Success criteria + QA scenarios

- Tier: HEAVY because this change touches external integrations, authentication/check semantics, durable lifecycle state, concurrency/transaction ordering, trusted configuration, and review containment.
- Criterion 1: exact-base and custody baseline is proven by `pwd -P`, `git rev-parse --show-toplevel`, `git branch --show-current`, `git rev-parse HEAD`, Made binary revision/help, read-only daemon status, tool checks, Herdr helper provisioning, and plan/brief inspection; PASS requires exact task worktree, exact base, preserved prior dirty count, no shared-daemon mutation, and captured phase-0 evidence.
- Criterion 2: strict GitHub and Codex external contracts pass via focused Go tests and the real Made binary against strict fakes; RED must fail on obsolete or invalid invocations and GREEN must accept only supported structured fields and outputs, with captured command output.
- Criterion 3: durable lifecycle contracts pass via focused Go integration tests and disposable Made homes, repositories, and process fixtures; RED/GREEN plus CLI/socket observations must prove submission refresh, exact run identity, decision timing/conflicts, cancellation, awaiting_merge success, idle/daemon-down distinction, fixed stages/current stage, evidence durability/retention/torn-tail recovery, restart, config enforcement, and reviewer containment.
- Criterion 4: local Made-only compatibility/build/test/vet/lint passes, changed scope is captured from the exact base, plan/evidence/checklist conventions are preserved, and a direct-PR branch is pushed/open with no forbidden repository, daemon, or pipeline action.
- Real-surface scenario for the CLI/data deliverable: run the real Made binary with disposable HOME/config/repository and strict fakes; PASS is exact structured JSON state/identity and expected exit code, captured in evidence.
- STOP: I'll stop right away when every requested contract has RED-to-GREEN evidence and real-surface PASS, all spawned resources have cleanup receipts, the branch is committed from the exact base, pushed, and its direct PR is open.

## Now

Phase 3 lifecycle and durability slices are GREEN in focused daemon, CLI,
configuration, evidence, reviewer-containment, orchestrator, and rebase tests;
disposable real-binary QA and final validation remain.

## Todo

- Read applicable skill bodies and record their use.
- Finish continuation gap discovery from brief/plan and source symbols.
- Provision named Herdr lab only after baseline and trap setup.
- Add and run RED tests before production edits.
- Implement minimal fixes with immediate GREEN and QA.
- Capture `evidence/phase-2-external-contracts.md` for the GitHub/CI GREEN slice.
- Update plan and evidence and run final validation.
- Commit, push branch, open direct PR, and append the done receipt.

## Findings

- Task worktree is `/Users/douglasjarquin/.herdr/worktrees/made/cs-made-remediation-continuation`.
- Branch is `cs/made-remediation-continuation`; HEAD and required base are `3e19ed9d598a68149da5a73949533e8095ca4403`.
- Task worktree was clean at bootstrap.
- Prior Made remediation worktree exists and has six porcelain entries; only existence, count, and HEAD were checked, not untracked artifact contents.
- Codegraph is available for this Made project; initial exploration found current review, decision, run-state, CI, and agent call surfaces.
- Made binary is `/Users/douglasjarquin/.local/bin/made`; `made --version`, `made version`, and `made --help` are unsupported and exit 2, so its revision/help contract needs discovery.
- Go is `go1.26.6 darwin/arm64`; git is 2.55.0; `gh-axi`, `herdr`, `codex`, and `golangci-lint` are installed; `chrome-devtools-axi` is not on PATH.
- The current Capo brief at `/Users/douglasjarquin/.consigliere/capos/made/data/made-remediation-continuation/brief.md` was reread before advancing; its binding gates are public structured contract, lifecycle and durability, evidence, semantic config, strict external compatibility, disposable live scenarios, and final validation.
- The current Capo brief forbids real-project validation, gate initialization, run submission, shared Made daemon lifecycle changes, default-branch pushes, merges, auto-merge, remote-branch deletion, and ask-user decisions.
- Phase 0 evidence is `evidence/phase-0-grounding-made-remediation-continuation.md`.
- Sparse supervisor receipt was appended as `working: [key=made-remediation-continuation] phase 0 grounding complete`.
- The named Herdr lab session is `cs-lab-made-remediation-9714-1438`, provisioned through the required helper with the EXIT teardown trap installed first.
- Memory-derived prior-run contract facts identify the intended public Made surface as `made capabilities --json`, `made run submit/status/list/cancel`, `made review decide`, and `made doctor --json`; exact run IDs and structured JSON are mandatory, and obsolete predecessor/global-latest behavior is rejected.
- Memory-derived prior-run facts also identify durable state/WAL and submission-spool replay, strict config, evidence redaction/retention, current Codex invocation, GitHub check/run handling, review containment, and real-binary compatibility as the Phase 1–3 continuation baseline to reproduce from source, without opening or copying the prior worktree.
- Read-only discovery lane 1 found `internal/github/client.go:70-120` uses mergeability and PR URLs for run operations, and `internal/agent/spawn.go:20-44` uses one undocumented invocation and loose raw JSON.
- Read-only discovery lane 2 found in-memory run state, queued cancellation loss, awaiting-merge terminal-event mismatch, missing current stage, overwriteable decisions, shallow snapshot slices, and lossy non-replayable mailbox behavior.
- Read-only discovery lane 3 found semantic config mostly satisfies its trust boundary, but unknown YAML switches are accepted, evidence writes are non-atomic, concurrent orphan publication loses a run, infrastructure failures can omit stage results, and reviewer auto-fix uses broad `git add -A`.
- Exact RED evidence is `evidence/phase-1-red-made-remediation-continuation.md`.
- Phase 2 GitHub and CI GREEN evidence is `evidence/phase-2-external-contracts.md`.
- The supported GitHub contract is `gh pr checks <pr> --json name,state,bucket,link`,
  with numeric workflow run IDs extracted from check links and explicit auth,
  check, log, and rerun errors.
- The supported Codex adapter invokes `exec --json --output-schema
  <schema> --output-last-message <file> --ephemeral -C <worktree> <task>` and
  parses only the required structured findings object.
- Claude is explicitly rejected at the Made agent boundary because the current
  supported structured contract is Codex-only; no generic agent compatibility
  shim was added.
- Focused agent and review happy-path GREEN evidence is in
  `evidence/phase-2-external-contracts.md`.
- Phase 3 focused lifecycle, durability, evidence, configuration, reviewer,
  orchestrator, and rebase evidence is in
  `evidence/phase-3-lifecycle-durability.md`.
- The durable run store uses a fsynced JSONL WAL plus atomic checkpoint and
  bounded compaction; a final malformed WAL record is ignored as a torn tail,
  while malformed non-final records fail open/recovery closed.
- The public run surface is `capabilities --json`, exact-ID
  `run submit/status/list/cancel`, `review.decide`, and structured `doctor
  --json`; the obsolete global-latest `status` command is rejected.
- `awaiting_merge` is non-terminal until an explicit `succeeded` transition,
  and daemon shutdown cancels only queued/running execution while preserving
  durable awaiting-merge records.
- Real Made binary manual QA passed against a disposable home at
  `evidence/phase-4-manual-qa.md`: capabilities, queued pre-drain submission,
  exact-ID status/list, obsolete-status rejection, doctor JSON, daemon restart
  recovery, and strict exact-ID error behavior were observed.
- The isolated Herdr helper probe confirmed named session
  `cs-lab-made-remediation-9714-1438` is running and compatible; final teardown
  remains pending until all validation and delivery work is complete.
- LSP diagnostics for the changed GitHub/CI production files and focused tests
  reported no errors or warnings; one non-blocking `stringsseq` hint remains in
  `internal/pipeline/ci/ci_contract_test.go`.
- Baseline isolated suite still has a pre-existing `internal/pipeline/rebase/TestRun_CleanRebaseProceeds` failure after Git-signing isolation; it is not hidden and remains a validation item.
- Installed Made contract discovery from the binary reports `made capabilities --json` with `schema_version`, `protocol_version`, and commands `run.submit`, `run.status`, `run.list`, `run.cancel`, `review.decide`, `doctor`; run states include `queued`, `running`, `awaiting_review`, `awaiting_merge`, `succeeded`, `failed`, `canceled`, and `superseded`; `execution_finished` is independent.

## Learnings

- Never inspect the retained prior worktree's untracked evidence.
- Do not use the shared Made daemon; use only read-only state checks and the named Herdr lab helper for task-specific lifecycle experiments.
