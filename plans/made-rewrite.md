# made: independent Go rewrite of no-mistakes, deeply integrated with herdr and consigliere

## TL;DR
> **Summary**: Build `made`, a from-scratch Go validation-gate daemon that is an independent synthesis of no-mistakes' pipeline concept (not a fork, not a vendored dependency), owns its own bare-gate-repo trust boundary, uses herdr purely as a live pane/terminal surface (never as the execution or trust channel), and fully replaces no-mistakes as consigliere's validation backend across all seven points where consigliere currently depends on no-mistakes.
> **Deliverables**: `made` Go module (daemon + CLI + socket API + 9-stage pipeline, GitHub-only, Claude+Codex-only); herdr pane-visibility integration; generated `skills/made/SKILL.md`; consigliere migration (`cs-made-lib.sh` shim + 7 updated call sites) that fully retires consigliere's no-mistakes integration.
> **Effort**: XL
> **Parallel**: YES - 6 implementation waves + 1 final verification wave
> **Critical Path**: gitgate + trusted-config (W1) -> socket API + evidence + run-manager (W2) -> pipeline stages (W3) -> GitHub delivery stages (W4) -> CLI + herdr pane surface + skill (W5) -> consigliere migration (W6)

## Context

### Original Request
Doug asked for a personal rewrite of `~/github/oss/no-mistakes` (a Go validation-gate CLI/daemon), staying in Go, under the explicit governing principle: **"made is an independent synthesis, not a dependency bundle or a one-to-one copy of any source."** It must integrate DEEPLY with `~/github/consigliere` (bash fleet supervisor) and `~/github/oss/herdr` (Rust terminal multiplexer), and be aware of `~/github/oss/lazycodex` + `~/github/oss/lazyclaudecode`, which currently have a real clash (both ship a plugin named "omo" that registers an MCP server named "lsp" - name collision, not a port/CI conflict).

### Interview Summary
Four architecture decisions were made via direct interview, after grounding in all four repos:

1. **Integration shape (made <-> consigliere)**: made runs a background daemon exposing a JSON-RPC-over-unix-socket API using the same method/params/id envelope idiom as herdr's own socket API (see `/Users/douglasjarquin/github/oss/herdr/src/api/schema.rs:40-45` for the serde tag/content shape to mirror in Go, not to be wire-compatible with - it is a stylistic/idiomatic match, not a shared protocol). This was chosen over: evolving no-mistakes' own internal/daemon+internal/ipc mailbox (proven, but not symmetric with herdr's idiom, and no-mistakes' own code cannot be reused verbatim per the independent-synthesis principle); consigliere's file-based `state/` mailbox convention (bash-native, but no live/streaming state); and plain CLI shellout (too shallow to call "deep integration" - consigliere would gain nothing beyond what it has today).
2. **Herdr delegation depth - RESOLVED AFTER METIS FOUND A BLOCKING CONTRADICTION**: the initial framing ("made drives herdr for worktree isolation") was proven infeasible - herdr's `worktree.create`/`worktree.remove` API explicitly rejects bare repositories (`/Users/douglasjarquin/github/oss/herdr/src/app/api/worktrees.rs`, `entry.is_bare` check) and only operates on non-bare source checkouts (`/Users/douglasjarquin/github/oss/herdr/src/worktree.rs:228-262`), while no-mistakes' entire trust boundary depends on a **bare** gate repo with a pre-receive admission hook (`/Users/douglasjarquin/github/oss/no-mistakes/internal/git/hook.go:17-85`, `/Users/douglasjarquin/github/oss/no-mistakes/internal/git/git.go:101` `ValidateBareRepository`, `:144` `InitBare`). herdr panes also cannot report an exit code (`pane.send_text`/`send_keys` return only terminal text), so they cannot be the actual gate-execution channel. **Final decision: made owns its own bare-gate-repo + worktree + trusted-config layer (independently written, not ported) and runs every gate command as a direct child process with process-group reaping. herdr is used exclusively as a live, human/agent-visible pane surface that tees command output for watching/attaching - never for worktree creation, never for pass/fail determination.**
3. **lazyharness MCP clash**: out of scope for made v1. made's v1 does not need to know lazycodex/lazyclaudecode exist.
4. **Adapter matrix**: trimmed to Doug's actual footprint. SCM: GitHub only (via `gh`, no adapter abstraction, no capability-negotiation layer - GitHub is assumed always capable of mergeable-state, check-logs, and check-rerun; any of those failing is a hard error). Agents: Claude + Codex only, no OpenCode/generic ACP layer.
5. **Test strategy**: TDD (RED-GREEN-REFACTOR) for every task, including the socket protocol layer.
6. **V1 scope**: full 9-stage pipeline (Intent -> Rebase -> Review -> Test -> Document -> Lint -> Push -> PR -> CI) in this one plan, sequenced into waves so each wave is a genuinely working increment.
7. **Migration scope - RESOLVED AFTER METIS SURFACED 7 SEAMS, NOT JUST ONE**: Metis found that consigliere's coupling to no-mistakes goes well beyond `.no-mistakes.yaml` - it includes the `no-mistakes` git remote convention, a CLI output parser pinned to installed v1.32.2, `~/.no-mistakes/logs/*/ci.log` parsing, log-scrape-based busy-detection, a `--mode no-mistakes` delivery mode, a hardcoded `$no-mistakes` skill string baked into three brief templates, and the `no-mistakes init`/`doctor` bootstrap flow. **Decision: full replacement.** This plan migrates all seven seams so made completely retires no-mistakes as consigliere's validation backend.
8. **Migration inventory correction - Momus review found the 7-seam inventory above was still incomplete.** A full `grep -rn "no-mistakes" /Users/douglasjarquin/github/consigliere/bin/` shows 17 files with real matches, not 5: the original list undercounted `bin/cs-nm-run-lib.sh` (a whole run-attribution library, not covered by any task), `bin/cs-teardown.sh` (20 matches - run conclusion at task teardown), `bin/cs-classify-lib.sh` (busy-state classification comments describing the exact behavior Task 28 changes), and a long tail of one-or-few-line references in `cs-board-capacity.sh`, `cs-deps-lib.sh`, `cs-delivery-lib.sh`, `cs-fleet-sync.sh`, `cs-harness-lib.sh`, `cs-pr-check.sh`, `cs-project-mode.sh`, `cs-promote.sh`, `cs-sessionstart-run.sh`, `cs-spawn.sh`, `cs-update.sh`. Tasks 29-35 below were added or widened to cover every one of these, with exact file:line citations taken from a fresh grep of the real repo (not estimated), so the Definition of Done's "zero `no-mistakes` matches in consigliere/bin/" is actually achievable.

### Metis Review (gaps addressed)
Metis (`omo:metis`) reviewed the draft against the actual source of all four repos and found 1 blocking contradiction (herdr/bare-repo, resolved above via decision 2), plus ambiguities, missing constraints, and execution risks. All are resolved in this plan as follows:

- **Trusted-branch invariant, fully specified** (was under-described): made's config loader enforces, unconditionally: (a) six fields - `Document`, `Review`, `DisableProjectSettings`, `NoCI`, `CI`, `Test.Evidence.Branch` - are always read from the trusted default-branch copy only, regardless of any repo-branch override; (b) `Commands`/`Agent`/`Agents` are trusted-only *unless* the trusted copy itself sets `allow_repo_commands: true`; (c) with no trusted copy at all, executable fields are forced empty (never silently fall back to the pushed branch's copy); (d) if the trusted copy cannot be read for any reason, the run aborts fail-closed (mirrors `/Users/douglasjarquin/github/oss/no-mistakes/internal/daemon/manager.go:229` and `:862` `assertGateTrustedConfigReadable`). See Task 3.
- **Command execution channel** (herdr panes leak the boundary / can't return exit codes): resolved by decision 2 above - direct child process, own process-group reaping, herdr pane is a tee'd view, never the execution path. See Task 4, Task 20.
- **Daemon lifecycle** (unspecified): made's daemon is a per-user singleton, started on-demand by the first CLI call via a flock-style OS file lock (independently implemented, same pattern-shape as no-mistakes' own singleton lock, not copied code), stopped via `made daemon stop` or automatically on idle timeout. See Task 5.
- **Socket auth model** (none stated): unix socket file created with `0600` permissions, owner-only, at `$MADE_HOME/daemon.sock` - no additional auth layer, matching herdr's own model (filesystem permissions only). See Task 6.
- **Protocol version policy** (herdr's `PROTOCOL_VERSION=20` requires exact match, `/Users/douglasjarquin/github/oss/herdr/src/protocol/wire.rs:16` and `:1009-1021`): made's herdr client pins a required herdr protocol floor and checks it via the `protocol` field in herdr's status response (`/Users/douglasjarquin/github/oss/herdr/src/api/status.rs:10`) before issuing any pane call; a mismatch degrades to "no live pane" (fail-open) rather than blocking the gate run, since herdr is not the trust boundary. made's own socket API is independently versioned (`made.protocol` field in every envelope) and made's own CLI enforces exact match against its daemon (this one fails closed - client/daemon version skew is a real correctness risk made controls end-to-end). See Task 6, Task 7.
- **herdr session identity / soldier-side restriction** (consigliere's `cs-brief.sh:335` forbids soldiers from making herdr calls scoped only by ambient `HERDR_SESSION`): made's herdr client always passes an explicit `--session made` (its own dedicated herdr session, namespaced separately from consigliere's `cs/<id>` soldier workspaces) on every call - never relies on ambient `HERDR_SESSION`. made's daemon runs as a host-level process, not inside a soldier pane, so it does not violate that constraint. See Task 7, Task 20.
- **Concurrency bounds** (unspecified): made's daemon serializes gate runs per-repo (one active worktree per bare gate repo at a time, additional pushes queue), matching the safety property no-mistakes provides today. See Task 5, Task 9.
- **PR stage vs. consigliere's merge-authority constraint** (`/Users/douglasjarquin/github/consigliere/plans/consigliere-lazy-harness-integration.md:23`, `:57-58`: delivery mode is the sole owner of push/PR/merge behavior): made's PR stage opens a pull request and stops - it never merges, never sets auto-merge, under any config. made's Push stage only pushes to the real remote; it does not gate on or trigger merge. This constraint is enforced in code, not just documentation. See Task 18.
- **Evidence store dual-mode** (draft said orphan-branch, consigliere's config says `store_in_repo: true` - not actually a contradiction, just two supported modes): made supports both - default is an orphan evidence branch in the target repo (never merged into shipped history); `store_in_repo: true` config redirects to an in-repo directory (matching consigliere's current `.no-mistakes/evidence` usage so the migration doesn't lose data). See Task 8.
- **Go toolchain baseline** (unspecified): Go 1.23+, standard `go.mod` module layout, `go test ./...` as the canonical test command, golangci-lint for static analysis. See Task 1.
- **`gh` auth precondition** (unspecified): made's GitHub client runs `gh auth status` as a preflight check before any Push/PR/CI stage; failure is a hard, clearly-messaged error, not a silent skip. See Task 16.
- **Rollback/cleanup contract** (unspecified): on any stage failure, made removes the worktree it created (bare-repo owned, so no herdr `workspace_id` dependency for cleanup) and closes any herdr pane it opened for that run; partial evidence up to the failure point is retained and marked incomplete. See Task 2, Task 20.
- **No TUI, but human approval still needed for ask-user findings**: made does not build a rich TUI (no bubbletea-style framework). Ask-user findings are presented as a plain-text CLI prompt in whatever terminal the `made` CLI client is attached to - which in consigliere's flow will typically be a herdr pane, so herdr still ends up serving as the human's window onto the approval prompt, but made itself only ever writes plain stdin/stdout, never pane-scrapes. See Task 22.
- **Reference citation discipline** (Metis: no CodeGraph index exists on made, no-mistakes, or herdr, so "follow the pattern in package X" is unusable): every task below cites concrete `file:line` locations in the source repos, not package names, per Metis's finding.

## Work Objectives

### Core Objective
Ship a working `made` daemon + CLI + socket API that independently re-implements no-mistakes' 9-stage validation-gate pipeline (GitHub + Claude/Codex only), uses herdr exclusively as a live pane-visibility surface, and fully replaces no-mistakes as consigliere's validation backend.

### Deliverables
- `made` Go module: `cmd/made` (CLI), `internal/gitgate`, `internal/config`, `internal/exec`, `internal/daemon`, `internal/api`, `internal/herdrclient`, `internal/evidence`, `internal/pipeline` (9 stages), `internal/agent` (Claude + Codex), `internal/github`, `internal/skill`.
- `skills/made/SKILL.md`, generated from `internal/skill/skill.go`, with a drift-check lint rule.
- `bin/cs-made-lib.sh` and `bin/cs-made-run-lib.sh` (renamed from `cs-nm-run-lib.sh`) in consigliere, plus updated `cs-home-seed.sh`, `cs-crew-state.sh`, `cs-watch.sh`, `cs-brief.sh`, `cs-bootstrap.sh`, `cs-teardown.sh`, `cs-classify-lib.sh`, `cs-delivery-lib.sh`, `cs-project-mode.sh`, `cs-board-capacity.sh`, `cs-pr-check.sh`, `cs-promote.sh`, `cs-spawn.sh`, `cs-fleet-sync.sh`, `cs-deps-lib.sh`, `cs-harness-lib.sh`, `cs-sessionstart-run.sh`, `cs-update.sh` - the full 17-file seam surface identified by grepping the real repo (see Context item 8) - that fully retire the no-mistakes integration.

### Definition of Done (verifiable conditions with commands)
- `cd made && go build ./... && go test ./...` passes with zero failures.
- `made gate init` in a scratch repo creates a bare gate repo and a working `made` git remote; `git push made <branch>` runs the full 9-stage pipeline end to end against a trivial passing change and reports success via `made status --json`.
- `made gate init` against a trivial failing test causes the Test stage to block the push (non-zero exit, clear message), and the target repo's real remote receives no new ref.
- With a herdr server running (`herdr server &`), a gate run opens a visible pane (`herdr pane list --session made` shows it); with herdr stopped, the same run still completes successfully (fail-open confirmed).
- `grep -rn "no-mistakes" /Users/douglasjarquin/github/consigliere/bin/` returns zero matches (Tasks 25-35 cover all 17 files identified by this exact grep against the real repo, including comment-only references, per Context item 8).
- `made pr` on a scratch branch opens a GitHub PR via `gh` and does not merge it, verified by `gh pr view --json state` reporting `OPEN`.

### Must Have
- Independent implementation of every component listed in Deliverables - no vendoring, no copy-pasted code from no-mistakes, herdr, or consigliere source.
- The four trusted-config rules from the Metis Review section, enforced with a failing-then-passing test for each.
- herdr integration that never blocks a gate run when herdr is unreachable (fail-open verified by an explicit test).
- PR stage that structurally cannot merge (no merge API call exists in the codebase for it to make).

### Must NOT Have
- No GitLab, Bitbucket, or Azure DevOps adapters, and no adapter abstraction interface for hypothetical future SCMs.
- No OpenCode or generic ACP agent adapter.
- No fix, workaround, or awareness logic for the lazycodex/lazyclaudecode MCP "lsp" name collision.
- No custom TUI framework.
- No worktree creation, pass/fail determination, or trust-boundary logic delegated to herdr.
- No backward-compatibility shim that keeps no-mistakes callable from consigliere after migration (this is a full replacement, not a dual-path fallback).

## Verification Strategy
> ZERO HUMAN INTERVENTION - all verification is agent-executed.
- Test decision: TDD (RED-GREEN-REFACTOR), Go standard `testing` package + `go test`. Every task's failing test is written and shown failing before the implementation task begins.
- QA policy: every task below has agent-executed QA scenarios (happy path + failure/edge case), run for real against the actual binary/socket/repo state - no `--dry-run` substitutes.
- Evidence: `evidence/task-{N}-{slug}.{ext}` per task, captured under this plan's evidence directory (not made's own evidence store, which is a separate runtime concept).

## Execution Strategy

### Parallel Execution Waves

**Wave 1 - Foundations** (parallel, no cross-dependencies): Tasks 1, 2, 3, 4, 5
**Wave 2 - Core services** (depend on Wave 1): Tasks 6, 7, 8, 9
**Wave 3 - Pipeline stages** (depend on Wave 2): Tasks 10, 11, 12, 13, 14, 15
**Wave 4 - GitHub delivery stages** (depend on Wave 3): Tasks 16, 17, 18, 19
**Wave 5 - CLI, herdr pane surface, skill** (depend on Wave 2 and relevant Wave 3/4 tasks): Tasks 20, 21, 22, 23
**Wave 6 - consigliere migration** (depend on Wave 5): Tasks 24, 25, 26, 27, 28, 29, 30, 31, 32, 33, 34, 35
**Final Verification Wave**: F1-F4

### Dependency Matrix (full, all tasks)

| Task | Depends On | Blocks |
|---|---|---|
| 1. Repo scaffold | - | 2, 3, 4, 5 |
| 2. Bare gate repo + worktree layer | 1 | 9, 10-19 |
| 3. Trusted-config loader | 1 | 10-19 |
| 4. Exec engine + process-group reaping | 1 | 10-19, 20 |
| 5. Daemon singleton lock + skeleton | 1 | 6, 9 |
| 6. Socket API envelope + versioning | 5 | 7, 21 |
| 7. herdr client library | 6 | 20 |
| 8. Evidence store (dual mode) | 2, 3 | 10-19 |
| 9. Run manager / event mailbox | 2, 5 | 10-19, 21 |
| 10. Intent stage | 3, 8, 9 | 11 |
| 11. Rebase stage | 2, 10 | 12 |
| 12. Review stage + agent adapters | 3, 4, 8, 11 | 13 |
| 13. Test stage | 4, 8, 12 | 14 |
| 14. Document stage | 8, 13 | 15 |
| 15. Lint stage | 8, 14 | 17 |
| 16. GitHub client wrapper | 1 | 17, 18, 19 |
| 17. Push stage | 8, 15, 16 | 18 |
| 18. PR stage (open-only) | 16, 17 | 19 |
| 19. CI stage | 16, 18 | 21 |
| 20. herdr pane visibility | 4, 7 | 21 |
| 21. CLI core commands | 6, 9, 19, 20 | 22, 24 |
| 22. CLI approval-prompt flow | 21 | 24 |
| 23. Skill generator + SKILL.md | 1 | 24 |
| 24. cs-made-lib.sh shim | 21, 22, 23 | 25, 26, 27, 28, 29, 30, 31, 32, 33, 34, 35 |
| 25. Git remote migration (cs-home-seed.sh remote seed) | 24 | F1 |
| 26. CLI/status JSON parsing + run-lookup migration (cs-crew-state.sh) | 24, 32 | F1 |
| 27. Log path migration (cs-crew-state.sh evidence lines) | 24 | F1 |
| 28. Busy-detection migration (cs-watch.sh) | 24 | F1 |
| 29. --mode wiring + mode-semantics migration (cs-delivery-lib.sh, cs-project-mode.sh, cs-brief.sh mode prose, cs-board-capacity.sh, cs-pr-check.sh, cs-promote.sh, cs-spawn.sh, cs-fleet-sync.sh, cs-home-seed.sh mode gate) | 24 | F1 |
| 30. Skill string migration (cs-brief.sh skill-invocation lines) | 24 | F1 |
| 31. init/doctor + MADE_DOWN probe migration (cs-home-seed.sh init/doctor, cs-bootstrap.sh) | 24 | F1 |
| 32. cs-nm-run-lib.sh -> cs-made-run-lib.sh rename | 24 | 26, F1 |
| 33. cs-teardown.sh migration | 24 | F1 |
| 34. cs-classify-lib.sh comment/logic migration | 24, 28 | F1 |
| 35. Long-tail sweep (cs-deps-lib.sh, cs-harness-lib.sh, cs-sessionstart-run.sh, cs-update.sh) | 24 | F1 |
| F1-F4 | all above | - |

## TODOs

- [x] 1. Repo scaffold and CI skeleton

  **What to do**: Initialize `go.mod` (module `github.com/douglasjarquin/made`, Go 1.23+), create the directory layout (`cmd/made`, `internal/gitgate`, `internal/config`, `internal/exec`, `internal/daemon`, `internal/api`, `internal/herdrclient`, `internal/evidence`, `internal/pipeline`, `internal/agent`, `internal/github`, `internal/skill`), add a `Makefile` with `test`, `lint`, `build`, `skill` targets, and a GitHub Actions workflow (`.github/workflows/ci.yml`) running `go build ./...`, `go test ./...`, `golangci-lint run`. Replace the current one-line `README.md` with a short project description stating the independent-synthesis principle verbatim.
  **Must NOT do**: Do not copy no-mistakes' `Makefile`, `.golangci.yml`, or workflow YAML files - write these fresh for made's own module layout.

  **Parallelization**: Can Parallel: YES | Wave 1 | Blocks: 2, 3, 4, 5 | Blocked By: none

  **References**:
  - Layout inspiration (concept only, not copied): `/Users/douglasjarquin/github/oss/no-mistakes/cmd/` and `/Users/douglasjarquin/github/oss/no-mistakes/internal/` top-level package list (cmd/no-mistakes, internal/pipeline, internal/daemon, internal/cli, internal/agent, internal/scm, internal/git, internal/config).
  - Existing empty repo state: `/Users/douglasjarquin/github/douglasjarquin/made/README.md` (current one-line content, to be replaced).

  **Acceptance Criteria**:
  - [ ] `go build ./...` succeeds with no packages yet implemented beyond stub `doc.go` files.
  - [ ] `.github/workflows/ci.yml` exists and references `go test ./...` and `golangci-lint run`.
  - [ ] `make test`, `make lint`, `make build`, `make skill` targets all exist and run without "target not found" errors (skill/test may no-op until later tasks land).

  **QA Scenarios**:
  ```
  Scenario: Fresh clone builds
    Tool: bash
    Steps: `git clone` the repo into a scratch dir, run `go build ./...`.
    Expected: Exit 0, no errors.
    Evidence: evidence/task-1-fresh-build.txt

  Scenario: Makefile targets exist
    Tool: bash
    Steps: Run `make -n test lint build skill` (dry-run).
    Expected: All four targets resolve without "No rule to make target" errors.
    Evidence: evidence/task-1-makefile-targets.txt
  ```

  **Commit**: YES | Message: `chore(made): scaffold Go module, CI, and directory layout` | Files: `go.mod`, `go.sum`, `Makefile`, `.github/workflows/ci.yml`, `README.md`, `cmd/made/doc.go`, `internal/*/doc.go`

- [x] 2. Bare gate repo + worktree layer (internal/gitgate)

  **What to do**: Implement `internal/gitgate` with: creating a bare git repo per project at `$MADE_HOME/gates/<repo-hash>/gate.git` (own naming/layout, independently designed); a pre-receive-hook-equivalent admission check installed into the bare repo that authenticates the pushing process against the daemon before accepting a ref update; cutting a disposable worktree from the bare repo for each validation run; and worktree cleanup (removal) on both success and failure paths. Write TDD tests first: a failing test asserting `gitgate.InitBare(path)` creates a valid bare repo, then implement; a failing test asserting an unauthenticated push is rejected by the hook, then implement; a failing test asserting worktree cleanup happens after a simulated stage failure, then implement.
  **Must NOT do**: Do not import or shell out to any no-mistakes binary or package. Do not use herdr's worktree API for this layer (see Metis-driven decision in Context - herdr rejects bare repos entirely).

  **Parallelization**: Can Parallel: YES | Wave 1 | Blocks: 9, 10-19 | Blocked By: 1

  **References**:
  - Bare-repo pattern to independently re-derive (read for the *concept* of what a bare gate repo + hook needs to guarantee, do not copy the code): `/Users/douglasjarquin/github/oss/no-mistakes/internal/git/git.go:53` (`RunBare`), `:101` (`ValidateBareRepository`), `:144` (`InitBare`); pre-receive hook admission flow: `/Users/douglasjarquin/github/oss/no-mistakes/internal/git/hook.go:17-85`; gate init entrypoint shape: `/Users/douglasjarquin/github/oss/no-mistakes/internal/cli/init.go:25`, `:88`.
  - herdr's incompatibility (why this must be made's own layer, not delegated): `/Users/douglasjarquin/github/oss/herdr/src/app/api/worktrees.rs` (`is_bare` rejection), `/Users/douglasjarquin/github/oss/herdr/src/worktree.rs:228-262` (worktree add requires a non-bare `source_checkout_path`), `:180-199` (dirty-checkout errors on remove).

  **Acceptance Criteria**:
  - [ ] `go test ./internal/gitgate/...` passes, including a test that an unauthenticated push is rejected.
  - [ ] `gitgate.InitBare` produces a repo where `git rev-parse --is-bare-repository` prints `true`.
  - [ ] A simulated stage failure leaves zero worktree directories behind (verified by listing `$MADE_HOME/gates/*/worktrees/`).

  **QA Scenarios**:
  ```
  Scenario: Bare gate repo accepts an authenticated push
    Tool: bash
    Steps: Call `gitgate.InitBare` via a small test CLI harness, push a branch through the authenticated path.
    Expected: Push accepted, worktree created under the gate's worktree dir.
    Evidence: evidence/task-2-authenticated-push.txt

  Scenario: Unauthenticated push rejected
    Tool: bash
    Steps: Attempt a raw `git push` directly to the bare repo, bypassing the daemon's admission path.
    Expected: Push rejected with a clear hook error message, no ref updated.
    Evidence: evidence/task-2-unauthenticated-push-rejected.txt
  ```

  **Commit**: YES | Message: `feat(gitgate): bare gate repo, admission hook, worktree lifecycle` | Files: `internal/gitgate/*.go`

- [x] 3. Trusted-vs-pushed config loader (internal/config)

  **What to do**: Implement a YAML config loader enforcing the four trusted-config rules from the Context/Metis Review section: (a) `Document`, `Review`, `DisableProjectSettings`, `NoCI`, `CI`, `Test.Evidence.Branch` are read only from the trusted default-branch copy, unconditionally; (b) `Commands`, `Agent`, `Agents` are trusted-only unless the trusted copy sets `allow_repo_commands: true`; (c) if there is no trusted copy, executable fields resolve to empty, never falling back to the pushed branch's copy; (d) if the trusted copy exists but cannot be read (permissions, parse error, missing file), the loader returns a fail-closed error that aborts the run - it must not silently proceed with defaults. Write each rule as a failing test first (four separate test cases minimum), then implement.
  **Must NOT do**: Do not add a "safe mode" fallback that runs with partial trust when the trusted copy is unreadable - rule (d) is fail-closed, no exceptions.

  **Parallelization**: Can Parallel: YES | Wave 1 | Blocks: 10-19 | Blocked By: 1

  **References**:
  - Exact rule set to re-derive independently (read the logic, write your own Go types and validation, do not copy): `/Users/douglasjarquin/github/oss/no-mistakes/internal/config/config.go:1466` (`EffectiveRepoConfig`), `:1510-1521` (`allow_repo_commands` conditional trust).
  - Fail-closed abort behavior to match: `/Users/douglasjarquin/github/oss/no-mistakes/internal/daemon/manager.go:229` and `:862` (`assertGateTrustedConfigReadable`).

  **Acceptance Criteria**:
  - [ ] `go test ./internal/config/...` passes with at least 4 test cases, one per trust rule, each written RED before GREEN.
  - [ ] A config with no trusted copy present resolves `Commands`/`Agent`/`Agents` to empty, verified by a test asserting `len(cfg.Commands) == 0`.
  - [ ] An unreadable trusted copy (simulated via a permissions-denied file) causes `LoadEffectiveConfig` to return a non-nil error, verified by a test.

  **QA Scenarios**:
  ```
  Scenario: allow_repo_commands opt-in honored
    Tool: bash
    Steps: Create a trusted config with `allow_repo_commands: true` and a pushed-branch config overriding `commands.test`; load effective config.
    Expected: Effective `commands.test` matches the pushed branch's override.
    Evidence: evidence/task-3-allow-repo-commands.txt

  Scenario: Unreadable trusted config aborts
    Tool: bash
    Steps: chmod 000 the trusted config file, attempt to load effective config.
    Expected: Loader returns an error; caller treats it as an abort, not a soft-fail default.
    Evidence: evidence/task-3-unreadable-trusted-config.txt
  ```

  **Commit**: YES | Message: `feat(config): trusted-vs-pushed config boundary with fail-closed loading` | Files: `internal/config/*.go`

- [x] 4. Command execution engine with process-group reaping (internal/exec)

  **What to do**: Implement `internal/exec` to run gate-stage commands as direct child processes (not via herdr panes - see Context decision 2): start each command in its own process group, capture stdout/stderr/exit code, and reap the entire process group on completion, timeout, or cancellation to prevent orphaned children. Write a failing test first that starts a process which spawns a grandchild, cancels the run, and asserts the grandchild is also terminated; then implement.
  **Must NOT do**: Do not route command execution through herdr's `pane.send_text`/`send_keys` - those cannot report an exit code and are not a valid execution channel for gate stages.

  **Parallelization**: Can Parallel: YES | Wave 1 | Blocks: 10-19, 20 | Blocked By: 1

  **References**:
  - Process-group reaping pattern to independently re-derive: `/Users/douglasjarquin/github/oss/no-mistakes/internal/procreap/` (read for the concept - process group creation via `Setpgid`, signal-based reaping on cleanup - do not copy the implementation).
  - Why herdr panes are unsuitable as the execution channel (exit-code gap): `/Users/douglasjarquin/github/oss/herdr/src/api/schema.rs` (pane methods are text-in/text-out, no structured exit-status return type defined anywhere in the schema).

  **Acceptance Criteria**:
  - [ ] `go test ./internal/exec/...` passes, including the grandchild-reaping test.
  - [ ] Running a command that exits 1 returns an `exec.Result` with `ExitCode == 1` and captured stderr.
  - [ ] Cancelling a running command via context cancellation leaves zero orphaned processes (verified via `pgrep` against a known test marker in the QA scenario).

  **QA Scenarios**:
  ```
  Scenario: Exit code propagation
    Tool: bash
    Steps: Run `internal/exec` against `sh -c "exit 3"`.
    Expected: Result.ExitCode == 3.
    Evidence: evidence/task-4-exit-code.txt

  Scenario: Cancellation reaps grandchildren
    Tool: bash
    Steps: Run a command that backgrounds a long-sleeping grandchild, cancel the context after 1s, then `pgrep` for the sleep process.
    Expected: pgrep finds nothing - grandchild was reaped.
    Evidence: evidence/task-4-cancellation-reaping.txt
  ```

  **Commit**: YES | Message: `feat(exec): child-process execution engine with process-group reaping` | Files: `internal/exec/*.go`

- [x] 5. Daemon singleton lock + skeleton (internal/daemon)

  **What to do**: Implement a per-user singleton daemon process: an OS-level exclusive file lock (flock-style) at `$MADE_HOME/daemon.lock` preventing two daemons from running concurrently; a `made daemon start`/`stop`/`status` command surface; graceful shutdown on SIGTERM; and an idle-timeout auto-stop. Write a failing test first asserting a second `daemon start` attempt while one is running fails with a clear "already running" error, then implement.
  **Must NOT do**: Do not reuse or import any code from no-mistakes' `internal/daemon` package - re-derive the singleton-lock concept independently in made's own package structure.

  **Parallelization**: Can Parallel: YES | Wave 1 | Blocks: 6, 9 | Blocked By: 1

  **References**:
  - Singleton-lock concept to independently re-derive (read for the invariant it must provide, not the code): `/Users/douglasjarquin/github/oss/no-mistakes/internal/daemon/manager.go` (singleton lock acquisition and lifecycle guard behavior described around the trusted-config assertions at `:229`, `:862`).

  **Acceptance Criteria**:
  - [ ] `go test ./internal/daemon/...` passes, including the double-start rejection test.
  - [ ] `made daemon start` followed by a second `made daemon start` in the same `$MADE_HOME` exits non-zero with an "already running" message.
  - [ ] `made daemon stop` followed by `made daemon status` reports "not running".

  **QA Scenarios**:
  ```
  Scenario: Double-start rejected
    Tool: bash
    Steps: `made daemon start &`, wait for ready, run `made daemon start` again.
    Expected: Second invocation exits non-zero, prints "already running".
    Evidence: evidence/task-5-double-start.txt

  Scenario: Graceful stop
    Tool: bash
    Steps: `made daemon start &`, then `made daemon stop`, then `made daemon status`.
    Expected: status reports not running; lock file removed.
    Evidence: evidence/task-5-graceful-stop.txt
  ```

  **Commit**: YES | Message: `feat(daemon): singleton daemon lifecycle with exclusive lock` | Files: `internal/daemon/*.go`, `cmd/made/daemon.go`

- [x] 6. Socket API envelope + versioning (internal/api)

  **What to do**: Implement a JSON-RPC-style envelope over a unix domain socket at `$MADE_HOME/daemon.sock` (created with `0600` permissions, owner-only), using a method/params/id request shape and a result/error/id response shape idiomatically similar to herdr's schema (method name string + typed params + id correlation) but with made's own Go types - not wire-compatible with herdr, not copied from its Rust definitions. Every envelope carries a `made.protocol` integer version field; the daemon rejects any client whose version does not exactly match (mirroring herdr's own exact-match policy). Write a failing test first for socket creation permissions (assert mode is `0600`), then for version-mismatch rejection, then implement.
  **Must NOT do**: Do not implement HTTP or TCP transport - unix socket only, matching herdr's own transport choice and Doug's "personal machine" usage model.

  **Parallelization**: Can Parallel: YES | Wave 2 | Blocks: 7, 21 | Blocked By: 5

  **References**:
  - Idiom to mirror stylistically (method/params/id envelope shape, serde tag/content pattern) - not to copy verbatim: `/Users/douglasjarquin/github/oss/herdr/src/api/schema.rs:40-45`.
  - Exact-match version policy to mirror as a design choice (herdr's own precedent for why exact match, not floor, is the right rejection rule): `/Users/douglasjarquin/github/oss/herdr/src/protocol/wire.rs:16` (`PROTOCOL_VERSION = 20`), `:1009-1021` (`check_client_version`).

  **Acceptance Criteria**:
  - [ ] `go test ./internal/api/...` passes, including socket-permission and version-mismatch tests.
  - [ ] `stat -f "%Lp" $MADE_HOME/daemon.sock` (or `stat -c` on Linux) reports `600`.
  - [ ] A client sending a mismatched `made.protocol` value receives a structured error response, not a silent hang or crash.

  **QA Scenarios**:
  ```
  Scenario: Socket permissions
    Tool: bash
    Steps: Start the daemon, stat the socket file.
    Expected: Mode 0600, owned by the invoking user.
    Evidence: evidence/task-6-socket-permissions.txt

  Scenario: Version mismatch rejected
    Tool: bash
    Steps: Send a raw JSON request over the socket with `made.protocol` set to an intentionally wrong value.
    Expected: Structured error response naming the version mismatch.
    Evidence: evidence/task-6-version-mismatch.txt
  ```

  **Commit**: YES | Message: `feat(api): unix-socket JSON envelope with exact-match protocol versioning` | Files: `internal/api/*.go`

- [x] 7. herdr client library (internal/herdrclient)

  **What to do**: Implement a Go client for herdr's own JSON-RPC socket API: connect to `HERDR_SOCKET_PATH` (or the default `~/.config/herdr/sockets/<session>.sock`), always pass an explicit `--session made` / equivalent session parameter on every call (never rely on ambient `HERDR_SESSION`), check herdr's advertised protocol version via its status response before issuing pane/workspace calls, and expose a small typed method set (`OpenPane`, `TailPane`, `ClosePane`) sufficient for Task 20's pane-visibility feature - not a full herdr API surface. Write a failing test first (using a fake herdr socket server in the test) asserting a protocol-mismatch causes `herdrclient.Connect` to return a "degraded, no pane" result rather than an error that would abort the caller, then implement.
  **Must NOT do**: Do not use ambient `HERDR_SESSION` for any call - consigliere's `cs-brief.sh:335` explicitly forbids soldier-side code from doing this, and made's daemon must set the same standard for itself even though it runs host-side, not soldier-side.

  **Parallelization**: Can Parallel: YES | Wave 2 | Blocks: 20 | Blocked By: 6

  **References**:
  - herdr's JSON-RPC method schema to target: `/Users/douglasjarquin/github/oss/herdr/src/api/schema.rs` (pane/workspace method definitions).
  - Explicit-session requirement, exact constraint text: `/Users/douglasjarquin/github/consigliere/bin/cs-brief.sh:335` (forbids "any herdr call scoped only by ambient or inline HERDR_SESSION").
  - Session-scoping helper pattern to independently re-derive: `/Users/douglasjarquin/github/consigliere/bin/cs-herdr-lib.sh:32` (`cs_herdr_session()`), `:36-37` (shellout convention).
  - Protocol version surfaced in status: `/Users/douglasjarquin/github/oss/herdr/src/api/status.rs:10`, `/Users/douglasjarquin/github/oss/herdr/src/api/server.rs:352`.
  - Headless server mode (for made auto-detecting herdr availability, not auto-starting it - see Task 20): `/Users/douglasjarquin/github/oss/herdr/src/main.rs:571`.

  **Acceptance Criteria**:
  - [ ] `go test ./internal/herdrclient/...` passes, including the protocol-mismatch-degrades test.
  - [ ] Every outgoing call in the package includes an explicit session parameter; a static check (test that greps generated request payloads) confirms no call omits it.
  - [ ] `herdrclient.Connect` against no running herdr socket returns a "not available" result, not a panic or unhandled error.

  **QA Scenarios**:
  ```
  Scenario: herdr available, session explicit
    Tool: bash
    Steps: Start `herdr server`, connect via herdrclient, open a pane, inspect the raw request payload.
    Expected: Payload includes an explicit session field set to "made"; pane opens successfully.
    Evidence: evidence/task-7-explicit-session.txt

  Scenario: herdr unavailable
    Tool: bash
    Steps: With no herdr socket present, call herdrclient.Connect.
    Expected: Returns a degraded/not-available result, no crash.
    Evidence: evidence/task-7-herdr-unavailable.txt
  ```

  **Commit**: YES | Message: `feat(herdrclient): explicit-session herdr socket client for pane visibility` | Files: `internal/herdrclient/*.go`

- [x] 8. Evidence store (dual mode: orphan-branch + in-repo)

  **What to do**: Implement `internal/evidence` supporting two storage modes selected by config: (1) default - an orphan git branch in the target repo holding run evidence (findings, logs, diffs), never merged into shipped history; (2) `store_in_repo: true` - an in-repo directory (default `.made/evidence`, configurable `dir`) matching the shape consigliere's current `.no-mistakes/evidence` usage expects, so the later consigliere migration (Task 27) does not lose continuity. Write a failing test first for each mode (assert evidence written in orphan mode is not reachable from the default branch's tree; assert evidence written in in-repo mode appears at the configured path), then implement.
  **Must NOT do**: Do not make orphan-branch evidence mergeable by default - it must require an explicit, separate operation to ever surface in shipped history, if ever.

  **Parallelization**: Can Parallel: YES | Wave 2 | Blocks: 10-19 | Blocked By: 2, 3

  **References**:
  - Orphan-branch evidence precedent (concept, not code): no-mistakes' evidence-on-orphan-branch behavior as described in its README/AGENTS.md (evidence collected on an orphan branch, never in shipped code history).
  - In-repo mode compatibility target: `/Users/douglasjarquin/github/consigliere/.no-mistakes.yaml` (`store_in_repo: true`, `dir` pointing at `.no-mistakes/evidence`).

  **Acceptance Criteria**:
  - [ ] `go test ./internal/evidence/...` passes for both modes.
  - [ ] Orphan-mode evidence is confirmed absent from `git ls-tree -r <default-branch>`.
  - [ ] In-repo mode evidence appears at the configured directory and is confirmed present in `git status` as an addable path.

  **QA Scenarios**:
  ```
  Scenario: Orphan mode isolation
    Tool: bash
    Steps: Run a validation with default config, inspect the default branch tree for evidence files.
    Expected: No evidence files present on the default branch; they exist only on the orphan evidence branch.
    Evidence: evidence/task-8-orphan-isolation.txt

  Scenario: In-repo mode compatibility
    Tool: bash
    Steps: Set `store_in_repo: true`, run a validation, check `.made/evidence/`.
    Expected: Evidence files present at the configured path.
    Evidence: evidence/task-8-in-repo-mode.txt
  ```

  **Commit**: YES | Message: `feat(evidence): dual-mode evidence storage (orphan branch / in-repo)` | Files: `internal/evidence/*.go`

- [x] 9. Run manager / event mailbox (internal/daemon)

  **What to do**: Implement the daemon's run manager: tracks in-flight and completed gate runs keyed by repo + branch, serializes concurrent runs per bare gate repo (one active worktree at a time per repo, additional pushes queue rather than run concurrently), and exposes a bounded event mailbox that the socket API (Task 6/21) can subscribe to for streaming run status. Write a failing test first asserting a second push to the same gate repo while a run is active is queued (not run concurrently, not rejected), then implement.
  **Must NOT do**: Do not allow two worktrees to be checked out simultaneously against the same bare gate repo - this is the concurrency bound Metis flagged as missing.

  **Parallelization**: Can Parallel: YES | Wave 2 | Blocks: 10-19, 21 | Blocked By: 2, 5

  **References**:
  - Concurrency/serialization requirement source: Metis gap-analysis finding "Concurrency bounds" (no equivalent in draft; this task is the resolution).
  - Bounded mailbox concept to independently re-derive: `/Users/douglasjarquin/github/oss/no-mistakes/internal/ipc/` (read for the "bounded mailbox overflow handling" concept only - do not copy).

  **Acceptance Criteria**:
  - [ ] `go test ./internal/daemon/...` (run-manager subset) passes, including the queuing-not-concurrent test.
  - [ ] Pushing twice in quick succession to the same gate repo results in two sequential runs, verified by non-overlapping worktree lifetimes in the event log.
  - [ ] A subscriber connected before a run starts receives every status transition for that run (no dropped events under normal load).

  **QA Scenarios**:
  ```
  Scenario: Sequential queuing under concurrent pushes
    Tool: bash
    Steps: Fire two `git push made <branch>` commands back to back against the same gate repo.
    Expected: Event log shows run 2 starting only after run 1's worktree is removed.
    Evidence: evidence/task-9-sequential-queuing.txt

  Scenario: Status stream completeness
    Tool: bash
    Steps: Subscribe to run events via the socket, trigger a run, collect all events.
    Expected: Received events cover every stage transition from Intent through the final stage, in order.
    Evidence: evidence/task-9-status-stream.txt
  ```

  **Commit**: YES | Message: `feat(daemon): per-repo run serialization and event mailbox` | Files: `internal/daemon/runmanager.go`, `internal/daemon/mailbox.go`

- [x] 10. Intent stage

  **What to do**: Implement the first pipeline stage: validate that the pushed branch's stated intent (a required commit-trailer or config field - define one, keep it simple) is present and non-empty before proceeding. Write a failing test first (missing intent blocks the pipeline with a clear message), then implement.
  **Must NOT do**: Do not attempt semantic validation of intent content (e.g. no LLM-based "does this intent match the diff" check) - v1 only checks presence, matching the narrowest useful slice of no-mistakes' Intent stage.

  **Parallelization**: Can Parallel: NO (sequential pipeline stage) | Wave 3 | Blocks: 11 | Blocked By: 3, 8, 9

  **References**:
  - Stage ordering and purpose (concept only): no-mistakes' fixed pipeline order, Intent as stage 1, per `/Users/douglasjarquin/github/oss/no-mistakes/README.md` and `/Users/douglasjarquin/github/oss/no-mistakes/AGENTS.md` pipeline description.

  **Acceptance Criteria**:
  - [ ] `go test ./internal/pipeline/intent/...` passes.
  - [ ] A push with no intent trailer/field is blocked with a message naming the missing intent.
  - [ ] A push with a non-empty intent proceeds to the Rebase stage.

  **QA Scenarios**:
  ```
  Scenario: Missing intent blocks
    Tool: bash
    Steps: Push a branch with no intent field set.
    Expected: Pipeline halts at Intent stage with a clear error.
    Evidence: evidence/task-10-missing-intent.txt

  Scenario: Present intent proceeds
    Tool: bash
    Steps: Push a branch with an intent field set, observe stage transition.
    Expected: Run manager event log shows transition from Intent to Rebase.
    Evidence: evidence/task-10-intent-proceeds.txt
  ```

  **Commit**: YES | Message: `feat(pipeline): Intent stage` | Files: `internal/pipeline/intent/*.go`

- [x] 11. Rebase stage

  **What to do**: Implement rebasing the pushed branch onto the current default branch inside the gate worktree, using made's own `internal/gitgate` worktree (not herdr). On conflict, halt the pipeline with the conflicting files listed. Write a failing test first (a branch that conflicts with the default branch halts with a clear file list), then implement.
  **Must NOT do**: Do not attempt automatic conflict resolution - conflicts are always a hard stop in v1.

  **Parallelization**: Can Parallel: NO (sequential pipeline stage) | Wave 3 | Blocks: 12 | Blocked By: 2, 10

  **References**:
  - Stage purpose (concept only): no-mistakes' Rebase stage as pipeline step 2, per its README pipeline description.
  - Worktree operations to use: this plan's own `internal/gitgate` (Task 2), not herdr.

  **Acceptance Criteria**:
  - [ ] `go test ./internal/pipeline/rebase/...` passes, including the conflict-halt test.
  - [ ] A cleanly-rebasable branch proceeds to Review with the worktree HEAD reflecting the rebase.
  - [ ] A conflicting branch halts with the exact list of conflicting file paths in the error.

  **QA Scenarios**:
  ```
  Scenario: Clean rebase proceeds
    Tool: bash
    Steps: Push a branch that rebases cleanly onto default.
    Expected: Worktree HEAD shows rebased commits; stage transitions to Review.
    Evidence: evidence/task-11-clean-rebase.txt

  Scenario: Conflicting rebase halts
    Tool: bash
    Steps: Push a branch that conflicts with a change already on default.
    Expected: Pipeline halts, error lists the conflicting file(s).
    Evidence: evidence/task-11-conflict-halt.txt
  ```

  **Commit**: YES | Message: `feat(pipeline): Rebase stage` | Files: `internal/pipeline/rebase/*.go`

- [x] 12. Review stage + agent adapters (Claude, Codex)

  **What to do**: Implement the Review stage: spawn an agent (Claude CLI or Codex CLI, selected via effective config's `Agent` field) as a direct child process via `internal/exec` (Task 4) to review the diff in the gate worktree, parse its findings, and apply auto-fixable findings as new commits in the worktree while surfacing ask-user findings for later human approval (Task 22). Implement `internal/agent` with two adapters only: Claude and Codex - no interface abstraction layer beyond what's needed to select between exactly these two. Write a failing test first using a scripted fake agent binary (mirroring no-mistakes' own testing approach of a deterministic CLI double) that returns a fixed findings payload, asserting auto-fixable findings are committed and ask-user findings are queued, then implement.
  **Must NOT do**: Do not build a generic multi-agent adapter interface designed for hypothetical future agents - hardcode exactly Claude and Codex per the trimmed-adapter-matrix decision.

  **Parallelization**: Can Parallel: NO (sequential pipeline stage) | Wave 3 | Blocks: 13 | Blocked By: 3, 4, 8, 11

  **References**:
  - Fake-agent test-double pattern to independently re-derive (concept: a deterministic CLI double reading YAML scenarios, logging invocations) - do not copy: `/Users/douglasjarquin/github/oss/no-mistakes/cmd/fakeagent/main.go`.
  - Findings taxonomy concept (auto-fixable vs. ask-user vs. blocking) to re-derive independently: no-mistakes' pipeline findings taxonomy as described in its AGENTS.md.
  - Session-per-run-not-per-turn pattern worth keeping (durable fixer session across fix turns, review turns session-free) - concept only, from no-mistakes' AGENTS.md description of agentic design.

  **Acceptance Criteria**:
  - [ ] `go test ./internal/pipeline/review/...` and `go test ./internal/agent/...` pass.
  - [ ] Using the fake-agent test double, an auto-fixable finding results in a new commit in the worktree.
  - [ ] An ask-user finding is queued (visible via `made status --json`) rather than silently applied or silently dropped.

  **QA Scenarios**:
  ```
  Scenario: Auto-fix applied
    Tool: bash
    Steps: Configure the fake agent binary to return an auto-fixable finding, run Review stage.
    Expected: Worktree gains a new commit implementing the fix.
    Evidence: evidence/task-12-autofix-applied.txt

  Scenario: Ask-user finding queued
    Tool: bash
    Steps: Configure the fake agent to return an ask-user finding, run Review stage, query `made status --json`.
    Expected: Finding appears in status output as pending approval, pipeline does not silently proceed past it.
    Evidence: evidence/task-12-ask-user-queued.txt
  ```

  **Commit**: YES | Message: `feat(pipeline): Review stage with Claude/Codex agent adapters` | Files: `internal/pipeline/review/*.go`, `internal/agent/*.go`

- [x] 13. Test stage

  **What to do**: Implement running the effective config's `Commands.Test` (from Task 3's trusted-config loader) inside the gate worktree via `internal/exec` (Task 4), capturing pass/fail and full output as evidence (Task 8). A non-zero exit blocks the pipeline. Write a failing test first (a test command that exits non-zero blocks with output captured in evidence), then implement.
  **Must NOT do**: Do not run the target repo's full test suite by default if `Commands.Test` specifies a focused subset - respect the configured command exactly, matching no-mistakes' "focused test execution, not full suite" design choice.

  **Parallelization**: Can Parallel: NO (sequential pipeline stage) | Wave 3 | Blocks: 14 | Blocked By: 4, 8, 12

  **References**:
  - Stage scope rationale (focused tests, not full suite - remote CI owns regression): `/Users/douglasjarquin/github/oss/no-mistakes/README.md` pipeline description, stage 4.

  **Acceptance Criteria**:
  - [ ] `go test ./internal/pipeline/test/...` passes, including the non-zero-exit-blocks test.
  - [ ] A passing `Commands.Test` command proceeds to Document; output is present in evidence store regardless of outcome.
  - [ ] A failing command blocks the pipeline and the real remote receives no push.

  **QA Scenarios**:
  ```
  Scenario: Failing test blocks push
    Tool: bash
    Steps: Configure `commands.test` to a script that exits 1, push a branch.
    Expected: Pipeline halts at Test stage, evidence contains full stdout/stderr, no push to real remote.
    Evidence: evidence/task-13-failing-test-blocks.txt

  Scenario: Passing test proceeds
    Tool: bash
    Steps: Configure `commands.test` to a script that exits 0, push a branch.
    Expected: Pipeline proceeds to Document stage.
    Evidence: evidence/task-13-passing-test-proceeds.txt
  ```

  **Commit**: YES | Message: `feat(pipeline): Test stage` | Files: `internal/pipeline/test/*.go`

- [x] 14. Document stage

  **What to do**: Implement checking changed files in the pushed branch against a documentation-placement policy (a simple configurable set of path-pattern rules - e.g. "changes under `api/` require a corresponding doc update under `docs/api/`"), producing ask-user findings for violations. Write a failing test first (a changed file matching a policy pattern with no corresponding doc update produces a finding), then implement.
  **Must NOT do**: Do not build a generic pluggable policy-rule engine - a fixed, simply-configured pattern-pair list is sufficient for v1.

  **Parallelization**: Can Parallel: NO (sequential pipeline stage) | Wave 3 | Blocks: 15 | Blocked By: 8, 13

  **References**:
  - Stage purpose (concept only): no-mistakes' Document stage, pipeline step 5, checking documentation placement policy against changed files, per its README pipeline description.

  **Acceptance Criteria**:
  - [ ] `go test ./internal/pipeline/document/...` passes.
  - [ ] A policy violation produces an ask-user finding, visible in `made status --json`.
  - [ ] No violation proceeds directly to Lint.

  **QA Scenarios**:
  ```
  Scenario: Policy violation flagged
    Tool: bash
    Steps: Configure a doc-placement rule, push a branch changing a matched path with no doc update.
    Expected: Ask-user finding present in status output.
    Evidence: evidence/task-14-policy-violation.txt

  Scenario: Compliant change proceeds
    Tool: bash
    Steps: Push a branch that includes the required doc update alongside the code change.
    Expected: No finding raised, pipeline proceeds to Lint.
    Evidence: evidence/task-14-compliant-proceeds.txt
  ```

  **Commit**: YES | Message: `feat(pipeline): Document stage` | Files: `internal/pipeline/document/*.go`

- [x] 15. Lint stage

  **What to do**: Implement running the effective config's `Commands.Lint` (falling back to combining with Document stage output only if `Commands.Lint` is unset, matching no-mistakes' documented fallback behavior) inside the gate worktree, blocking on non-zero exit. Write a failing test first, then implement.
  **Must NOT do**: Do not hardcode a specific linter - always run the configured `Commands.Lint` command, matching the trusted-config boundary from Task 3 (Lint's command is drawn from `Commands`, which is trusted-only unless `allow_repo_commands: true`).

  **Parallelization**: Can Parallel: NO (sequential pipeline stage) | Wave 3 | Blocks: 17 | Blocked By: 8, 14

  **References**:
  - Fallback behavior (Lint combined with Document when `commands.lint` unset): `/Users/douglasjarquin/github/oss/no-mistakes/README.md` pipeline description, stage 6.

  **Acceptance Criteria**:
  - [ ] `go test ./internal/pipeline/lint/...` passes.
  - [ ] A failing lint command blocks the pipeline before Push.
  - [ ] `Commands.Lint` unset falls back to no-op-with-warning (not a hard failure) when Document already covers it, matching the documented fallback.

  **QA Scenarios**:
  ```
  Scenario: Failing lint blocks
    Tool: bash
    Steps: Configure `commands.lint` to a script that exits 1, push a branch.
    Expected: Pipeline halts before Push stage.
    Evidence: evidence/task-15-failing-lint-blocks.txt

  Scenario: Unset lint falls back
    Tool: bash
    Steps: Leave `commands.lint` unset, push a branch.
    Expected: No hard failure attributable to a missing lint command; pipeline proceeds.
    Evidence: evidence/task-15-unset-lint-fallback.txt
  ```

  **Commit**: YES | Message: `feat(pipeline): Lint stage` | Files: `internal/pipeline/lint/*.go`

- [x] 16. GitHub client wrapper (internal/github)

  **What to do**: Implement a thin wrapper around the `gh` CLI (shelled via `internal/exec`) for: `gh auth status` preflight, PR creation, mergeable-state query, check-log fetch, and check rerun - the exact operations no-mistakes' capability-negotiated SCM interface provides for GitHub, but here hardcoded and assumed always available (no capability negotiation, no `ErrUnsupported`, since GitHub is the only backend). Write a failing test first for the preflight check (missing/expired `gh` auth produces a clear, immediate error before any other GitHub call is attempted), then implement.
  **Must NOT do**: Do not implement a `Capabilities()` method or any interface designed to support a second SCM backend - this is GitHub-only, unconditionally.

  **Parallelization**: Can Parallel: YES | Wave 4 | Blocks: 17, 18, 19 | Blocked By: 1

  **References**:
  - Operations to reproduce (concept, GitHub-only slice): `/Users/douglasjarquin/github/oss/no-mistakes/internal/scm/github/` (PR creation, check polling) - read for what operations are needed, write fresh Go using `gh` directly rather than porting any GitHub API client code.
  - Capability-negotiation pattern being deliberately dropped (for contrast/awareness only): `/Users/douglasjarquin/github/oss/no-mistakes/internal/scm/host.go:175-210` (`Capabilities()`, `ErrUnsupported`, `CheckRerunner` type assertion) - made does not implement this; GitHub operations are unconditional.

  **Acceptance Criteria**:
  - [ ] `go test ./internal/github/...` passes, including the auth-preflight test.
  - [ ] With `gh auth status` failing, any GitHub client call returns an immediate, clearly-worded error without attempting the operation.
  - [ ] With `gh` authenticated, a test PR creation against a scratch repo succeeds and returns the PR URL.

  **QA Scenarios**:
  ```
  Scenario: Auth preflight failure
    Tool: bash
    Steps: Simulate `gh auth status` failing (e.g. `GH_TOKEN=invalid`), call any github client method.
    Expected: Immediate clear error naming the auth failure, no partial API call attempted.
    Evidence: evidence/task-16-auth-preflight-failure.txt

  Scenario: PR creation succeeds
    Tool: bash
    Steps: With valid `gh` auth, create a PR against a scratch repo/branch.
    Expected: PR URL returned, `gh pr view --json state` shows OPEN.
    Evidence: evidence/task-16-pr-creation.txt
  ```

  **Commit**: YES | Message: `feat(github): gh-CLI-backed GitHub client wrapper` | Files: `internal/github/*.go`

- [x] 17. Push stage

  **What to do**: Implement pushing the validated, rebased worktree branch to the repo's real remote (the one made's `made` remote sits in front of - resolved from the worktree's origin at runtime, credentials never persisted, matching no-mistakes' redacted-URL discipline). Write a failing test first (push to a remote requiring auth with no credentials available fails clearly, no partial state left behind), then implement.
  **Must NOT do**: Do not store credentials in made's own DB/state - resolve them from the worktree's git config at push time only, matching no-mistakes' credential-handling discipline.

  **Parallelization**: Can Parallel: NO (sequential pipeline stage) | Wave 4 | Blocks: 18 | Blocked By: 8, 15, 16

  **References**:
  - Credential discipline to independently re-derive (concept: stored URLs redacted, credentials recovered from worktree origin at runtime) - do not copy: no-mistakes' README/AGENTS.md description of credential handling.

  **Acceptance Criteria**:
  - [ ] `go test ./internal/pipeline/push/...` passes, including the no-credentials-fails-clean test.
  - [ ] A successful push updates the real remote's ref, verified by `git ls-remote`.
  - [ ] No credential material appears in made's run database or logs (grep evidence for secrets patterns as part of the QA scenario).

  **QA Scenarios**:
  ```
  Scenario: Successful push updates real remote
    Tool: bash
    Steps: Run a full validated pipeline against a scratch repo with a real remote, inspect `git ls-remote origin` before and after.
    Expected: Ref updated to the new commit after Push stage.
    Evidence: evidence/task-17-push-updates-remote.txt

  Scenario: No credential leakage
    Tool: bash
    Steps: After a push, grep made's logs/DB for the remote's embedded credentials (if any) or token strings.
    Expected: No matches found.
    Evidence: evidence/task-17-no-credential-leakage.txt
  ```

  **Commit**: YES | Message: `feat(pipeline): Push stage` | Files: `internal/pipeline/push/*.go`

- [x] 18. PR stage (open-only, structurally cannot merge)

  **What to do**: Implement opening a GitHub PR via `internal/github` (Task 16) with evidence links included in the PR body, after a successful Push. This stage's code has no path that calls any merge-capable `gh`/GitHub API operation - not gated behind a flag, structurally absent, so that consigliere's merge-authority constraint (`--mode` is sole owner of push/PR/merge behavior) cannot be violated even by misconfiguration. Write a failing test first asserting the PR-stage package contains no reference to a merge operation (a test that fails if a merge call is ever added, e.g. by asserting the set of `internal/github` methods called by this package excludes any merge method), then implement PR creation.
  **Must NOT do**: Do not add a `--ship`/`--merge`/auto-merge config option to this stage, ever - this is a hard constraint from consigliere's existing lazy-harness integration plan, not a v1-only limitation.

  **Parallelization**: Can Parallel: NO (sequential pipeline stage) | Wave 4 | Blocks: 19 | Blocked By: 16, 17

  **References**:
  - Merge-authority constraint, exact source: `/Users/douglasjarquin/github/consigliere/plans/consigliere-lazy-harness-integration.md:23` and `:57-58` ("Delivery (`--mode`) remains the sole owner of push/PR/merge behavior").
  - PR body / evidence-link pattern (concept only): no-mistakes' "Auto-create PR with evidence links" stage description in its README pipeline list, stage 8.

  **Acceptance Criteria**:
  - [ ] `go test ./internal/pipeline/pr/...` passes, including the no-merge-capability static-assertion test.
  - [ ] A successful run opens a PR containing a link to the run's evidence.
  - [ ] `gh pr view --json state` on the opened PR reports `OPEN`, never `MERGED`, immediately after the stage completes.

  **QA Scenarios**:
  ```
  Scenario: PR opened with evidence link
    Tool: bash
    Steps: Run the full pipeline through PR stage against a scratch repo.
    Expected: PR body contains a working link/reference to the run's evidence location.
    Evidence: evidence/task-18-pr-with-evidence.txt

  Scenario: PR never merged
    Tool: bash
    Steps: Immediately after PR stage completes, `gh pr view --json state`.
    Expected: State is OPEN.
    Evidence: evidence/task-18-pr-open-not-merged.txt
  ```

  **Commit**: YES | Message: `feat(pipeline): PR stage (open-only, no merge capability)` | Files: `internal/pipeline/pr/*.go`

- [x] 19. CI stage

  **What to do**: Implement polling the opened PR's GitHub Actions checks via `internal/github` (Task 16), with a bounded auto-rerun budget for check failures classified as transient (matching no-mistakes' "auto-rerun transient failures within budget" behavior). Write a failing test first (a check that exceeds the rerun budget surfaces as a final failure, not an infinite retry loop), then implement.
  **Must NOT do**: Do not rerun indefinitely - the budget must be a hard, configurable cap enforced in code.

  **Parallelization**: Can Parallel: NO (sequential pipeline stage) | Wave 4 | Blocks: 21 | Blocked By: 16, 18

  **References**:
  - Stage purpose and rerun-budget concept (no-mistakes' description, stage 9): `/Users/douglasjarquin/github/oss/no-mistakes/README.md` pipeline description.

  **Acceptance Criteria**:
  - [ ] `go test ./internal/pipeline/ci/...` passes, including the budget-exhaustion test.
  - [ ] A transient failure (simulated) within budget triggers exactly one rerun and then succeeds.
  - [ ] A persistent failure exhausts the budget and reports final failure with the check's log excerpt attached.

  **QA Scenarios**:
  ```
  Scenario: Transient failure recovers within budget
    Tool: bash
    Steps: Simulate a check that fails once then passes on rerun.
    Expected: CI stage reports success after exactly one rerun, logged.
    Evidence: evidence/task-19-transient-recovery.txt

  Scenario: Budget exhaustion surfaces final failure
    Tool: bash
    Steps: Simulate a check that always fails, exceeding the rerun budget.
    Expected: CI stage reports final failure, includes check log excerpt.
    Evidence: evidence/task-19-budget-exhaustion.txt
  ```

  **Commit**: YES | Message: `feat(pipeline): CI stage with bounded auto-rerun` | Files: `internal/pipeline/ci/*.go`

- [x] 20. herdr pane visibility integration

  **What to do**: Wire `internal/herdrclient` (Task 7) into the pipeline runner so that each gate run, if herdr is available, opens a pane in the "made" herdr session and tees live command output (from `internal/exec`, Task 4) into it for human/agent viewing/attaching - purely a visual mirror, never the execution or trust channel. If herdr is unavailable or its protocol doesn't match, the run proceeds identically with a logged fail-open warning, never blocking. On stage failure or run completion, the opened pane is closed/marked done. Write a failing test first asserting a run with herdr forcibly unavailable completes with identical pass/fail results as one with herdr available, then implement.
  **Must NOT do**: Do not make herdr availability a precondition for any gate run - fail-open is mandatory, verified by test, not just by manual observation.

  **Parallelization**: Can Parallel: YES | Wave 5 | Blocks: 21 | Blocked By: 4, 7

  **References**:
  - Fail-open requirement source: this plan's Context/Metis Review section ("herdr integration that never blocks a gate run when herdr is unreachable").
  - Pane tee mechanism: `internal/herdrclient.OpenPane`/`TailPane` (Task 7).

  **Acceptance Criteria**:
  - [ ] `go test ./internal/pipeline/...` (herdr-visibility subset) passes, including the fail-open equivalence test.
  - [ ] With herdr running, `herdr pane list --session made` shows an active pane during a run and no pane after completion.
  - [ ] With herdr stopped, the identical run completes with the same pass/fail outcome and timing order of stages.

  **QA Scenarios**:
  ```
  Scenario: Pane visible during run
    Tool: bash
    Steps: Start herdr server, trigger a gate run, run `herdr pane list --session made` mid-run.
    Expected: Pane present, tailing live stage output.
    Evidence: evidence/task-20-pane-visible.txt

  Scenario: Fail-open with herdr down
    Tool: bash
    Steps: Stop/do not start herdr, trigger the identical gate run.
    Expected: Run completes with the same result as the herdr-up scenario; log shows a fail-open warning.
    Evidence: evidence/task-20-fail-open.txt
  ```

  **Commit**: YES | Message: `feat(pipeline): herdr pane visibility with fail-open guarantee` | Files: `internal/pipeline/herdrview.go`

- [x] 21. CLI core commands (cmd/made)

  **What to do**: Implement the `made` CLI as a thin socket client: `made gate init`, `made status [--json]`, `made daemon start|stop|status` (delegating to Task 5), `made pr`, `made doctor` (health-check: daemon reachable, `gh` authenticated, herdr reachable-or-not reported informationally). `made status --json` must expose enough structured detail (run state, per-stage results, pending ask-user findings) to fully replace no-mistakes' TOON-format status output for consigliere's later migration (Task 26) - JSON, not TOON, since TOON was no-mistakes' own convention and made is not obligated to copy it. Write a failing test first asserting `made status --json` output validates against a defined JSON schema, then implement.
  **Must NOT do**: Do not implement a TOON output format - JSON only, this is a deliberate synthesis choice, not an oversight.

  **Parallelization**: Can Parallel: NO (depends on socket API and run manager) | Wave 5 | Blocks: 22, 24 | Blocked By: 6, 9, 19, 20

  **References**:
  - CLI surface being replaced (concept, not copied) - TOON output format consigliere currently parses: `/Users/douglasjarquin/github/consigliere/bin/cs-crew-state.sh:223`, `:254`, `:264` (`no-mistakes runs`, `no-mistakes axi status`, parsed as TOON, pinned to installed v1.32.2).
  - Doctor/health-check precedent (concept): `/Users/douglasjarquin/github/oss/no-mistakes/internal/cli/` `doctor` subcommand purpose, per no-mistakes' CLI subcommand list (init, daemon, axi, status, doctor, update, skill).

  **Acceptance Criteria**:
  - [ ] `go test ./cmd/made/...` passes, including the JSON-schema validation test.
  - [ ] `made status --json` output validates against the defined schema and includes run state, stage results, and pending ask-user findings.
  - [ ] `made doctor` reports daemon/gh/herdr reachability accurately in a scenario where each is deliberately down.

  **QA Scenarios**:
  ```
  Scenario: Status JSON schema validity
    Tool: bash
    Steps: Trigger a run, call `made status --json`, validate against the schema.
    Expected: Valid JSON matching the schema, includes all required fields.
    Evidence: evidence/task-21-status-json-schema.txt

  Scenario: Doctor reports accurately
    Tool: bash
    Steps: Stop the daemon, run `made doctor`.
    Expected: Reports daemon unreachable, does not falsely report other checks as failed.
    Evidence: evidence/task-21-doctor-accuracy.txt
  ```

  **Commit**: YES | Message: `feat(cli): made CLI core commands with JSON status output` | Files: `cmd/made/*.go`

- [x] 22. CLI approval-prompt flow for ask-user findings

  **What to do**: Implement a plain-text stdin/stdout approval prompt (no TUI framework) in `made status` / a dedicated `made review` command: lists pending ask-user findings from Review (Task 12) and Document (Task 14) stages, lets the operator approve/reject each via simple text input, and resumes the pipeline accordingly. When invoked inside a herdr pane (the visibility pane from Task 20, or any terminal), this "just works" as plain terminal I/O - made does not pane-scrape or otherwise treat herdr specially here. Write a failing test first (a rejected finding halts the pipeline with the rejection recorded in evidence; an approved finding applies and resumes), then implement.
  **Must NOT do**: Do not build this as a rich TUI (no bubbletea/tview) - plain sequential text prompts only, per the no-TUI decision in Context.

  **Parallelization**: Can Parallel: NO (depends on CLI core) | Wave 5 | Blocks: 24 | Blocked By: 21

  **References**:
  - What this replaces (concept only): no-mistakes' `internal/tui` interactive approval flow, per its package list - made does not port this, it re-derives the minimal "ask, read a line, resume" loop needed.

  **Acceptance Criteria**:
  - [ ] `go test ./cmd/made/...` (approval subset) passes, including reject-halts and approve-resumes tests.
  - [ ] Running `made review` against a run with pending findings presents each finding as plain text with a clear approve/reject prompt.
  - [ ] A rejection is recorded in evidence and the pipeline does not proceed past that stage.

  **QA Scenarios**:
  ```
  Scenario: Approve resumes pipeline
    Tool: tmux
    Steps: `tmux new-session -d -s made-qa-approve`, trigger a run with a pending ask-user finding, `send-keys` an approval response, capture pane.
    Expected: Pipeline resumes and completes; evidence shows the approval.
    Evidence: evidence/task-22-approve-resumes.txt

  Scenario: Reject halts pipeline
    Tool: tmux
    Steps: Same setup, `send-keys` a rejection response, capture pane.
    Expected: Pipeline halts at that stage; evidence shows the rejection and reason if provided.
    Evidence: evidence/task-22-reject-halts.txt
  ```

  **Commit**: YES | Message: `feat(cli): plain-text approval prompt for ask-user findings` | Files: `cmd/made/review.go`

- [x] 23. Skill generator + skills/made/SKILL.md + drift lint

  **What to do**: Implement `internal/skill` holding the skill body as a Go source constant, a `cmd/genskill` (or `make skill` target) that renders it to a committed `skills/made/SKILL.md`, and a lint check that fails CI if the committed file drifts from the generated output - mirroring no-mistakes' generated-skill-file pattern (a pattern worth keeping, since it is a build-process discipline, not proprietary logic). The skill enables `/made [<task>]` invocation from Claude Code and Codex. Write a failing test first (intentionally drift the committed file, assert `make lint` fails), then implement the generator and regenerate correctly.
  **Must NOT do**: Do not hand-edit `skills/made/SKILL.md` directly in normal workflow - it must always be regenerated from `internal/skill/skill.go`.

  **Parallelization**: Can Parallel: YES | Wave 5 | Blocks: 24 | Blocked By: 1

  **References**:
  - Pattern to independently re-derive (concept: source-of-truth Go constant, generator, committed output, drift lint) - do not copy the generator code itself: `/Users/douglasjarquin/github/oss/no-mistakes/internal/skill/skill.go`, `/Users/douglasjarquin/github/oss/no-mistakes/cmd/genskill/main.go`, `/Users/douglasjarquin/github/oss/no-mistakes/skills/no-mistakes/SKILL.md`.

  **Acceptance Criteria**:
  - [ ] `go test ./internal/skill/...` passes.
  - [ ] `make skill` regenerates `skills/made/SKILL.md` byte-identical to a fresh render from `internal/skill/skill.go`.
  - [ ] `make lint` fails when the committed `SKILL.md` is manually drifted from the generator's output, and passes when they match.
    *(Delivered via `make test` instead: the drift check lives in `internal/skill`'s `TestCommittedSkillFileMatchesGenerator`, so `go test`/CI catches drift rather than `make lint`. Disclosed in evidence/task-23-drift-detected.txt; CI-effective either way.)*

  **QA Scenarios**:
  ```
  Scenario: Drift detected
    Tool: bash
    Steps: Manually edit skills/made/SKILL.md, run `make lint`.
    Expected: Lint fails, names the drifted file.
    Evidence: evidence/task-23-drift-detected.txt

  Scenario: Regeneration matches
    Tool: bash
    Steps: Run `make skill`, diff the output against git HEAD's version.
    Expected: No diff.
    Evidence: evidence/task-23-regeneration-matches.txt
  ```

  **Commit**: YES | Message: `feat(skill): generated made skill file with drift lint` | Files: `internal/skill/*.go`, `cmd/genskill/main.go`, `skills/made/SKILL.md`, `Makefile`

- [x] 24. cs-made-lib.sh shim (consigliere)

  **What to do**: In the consigliere repo, create `bin/cs-made-lib.sh` following consigliere's existing naming/function convention (`cs_<area>_<verb>`, one file per external tool, mirroring `cs-herdr-lib.sh`'s structure), providing thin wrapper functions that shell out to the `made` CLI (`made status --json`, `made gate init`, `made doctor`, `made daemon start/stop`, `made axi abort` for Task 33's teardown use) for use by the other consigliere scripts this plan updates (Tasks 25-35). Write a failing test first (a consigliere bats/shell test asserting `cs_made_status` returns parsed JSON fields), then implement.
  **Must NOT do**: Do not put any made-specific logic directly into `cs-crew-state.sh`/`cs-watch.sh`/etc. - those scripts must call through this shim, matching the existing `cs-herdr-lib.sh` separation of concerns.

  **Parallelization**: Can Parallel: NO (all Wave 6 tasks depend on this shim) | Wave 6 | Blocks: 25, 26, 27, 28, 29, 30, 31, 32, 33, 34, 35 | Blocked By: 21, 22, 23

  **References**:
  - Naming/structure convention to follow exactly: `/Users/douglasjarquin/github/consigliere/bin/cs-herdr-lib.sh` (function naming, shellout pattern at `:36-37`, session-scoping helper at `:32`).
  - Existing no-mistakes shim being replaced (for inventory of what functions are needed): `/Users/douglasjarquin/github/consigliere/bin/cs-crew-state.sh:223,254,264,295` (all current no-mistakes call sites - each needs a `cs-made-lib.sh` equivalent).

  **Acceptance Criteria**:
  - [ ] Shell test suite for `cs-made-lib.sh` passes (using consigliere's existing test framework under `tests/`).
  - [ ] `shellcheck bin/cs-made-lib.sh` reports zero issues.
  - [ ] Every no-mistakes call site identified in Task 24's references has a corresponding `cs_made_*` function available.

  **QA Scenarios**:
  ```
  Scenario: Status parsing
    Tool: bash
    Steps: With a made daemon running and a completed run, call `cs_made_status <repo>`.
    Expected: Returns parsed fields matching `made status --json` output.
    Evidence: evidence/task-24-status-parsing.txt

  Scenario: Shellcheck clean
    Tool: bash
    Steps: `shellcheck bin/cs-made-lib.sh`.
    Expected: Zero findings.
    Evidence: evidence/task-24-shellcheck-clean.txt
  ```

  **Commit**: YES | Message: `feat(consigliere): add cs-made-lib.sh shim for made CLI integration` | Files: `bin/cs-made-lib.sh` (in consigliere repo)

- [x] 25. Git remote migration (no-mistakes remote -> made remote)

  **What to do**: In consigliere, update `bin/cs-home-seed.sh:475` (the seed step that wires a project's `no-mistakes` git remote) to instead seed a `made` remote pointing at `made gate init`'s bare repo. Write a failing shell test first (seeding a fresh scratch project asserts a `made` remote exists and no `no-mistakes` remote is created), then implement.
  **Must NOT do**: Do not seed both remotes "for safety" - this is a full replacement, not a dual-path fallback, per the Must-NOT-Have list.

  **Parallelization**: Can Parallel: YES | Wave 6 | Blocks: F1 | Blocked By: 24

  **References**:
  - Exact call site to change: `/Users/douglasjarquin/github/consigliere/bin/cs-home-seed.sh:475`.

  **Acceptance Criteria**:
  - [ ] Shell test suite passes: seeding a scratch project results in a `made` git remote, verified by `git remote -v`.
  - [ ] `grep -n "no-mistakes" bin/cs-home-seed.sh` returns no remote-seeding references (comments explicitly marked as migration notes are the only allowed exception).

  **QA Scenarios**:
  ```
  Scenario: Fresh project seeds made remote
    Tool: bash
    Steps: Run the updated seed step against a scratch project directory.
    Expected: `git remote -v` shows a `made` remote, no `no-mistakes` remote.
    Evidence: evidence/task-25-made-remote-seeded.txt
  ```

  **Commit**: YES | Message: `fix(consigliere): seed made remote instead of no-mistakes remote` | Files: `bin/cs-home-seed.sh` (consigliere repo)

- [x] 26. CLI/status JSON parsing migration (replacing TOON parsing)

  **What to do**: In consigliere, update `bin/cs-crew-state.sh` to call `cs_made_status`/`cs_made_runs` (from Task 24's shim) and parse made's JSON output via `jq`, replacing the current TOON-format parsing of `no-mistakes runs`/`no-mistakes axi status` pinned to installed v1.32.2, and update the file's own comments describing this contract. This includes updating the `source bin/cs-nm-run-lib.sh` line at `:65` to `source bin/cs-made-run-lib.sh` once Task 32 lands, and renaming every `cs_nm_*` call reference (including at `:172-179,270,279-283,356`, not just `cs_nm_run`/`cs_nm_trim`) to the corresponding `cs_made_*` names Task 32 produces. Write a failing shell test first (asserting the function that previously parsed TOON now correctly parses made's JSON schema from Task 21), then implement.
  **Must NOT do**: Do not keep a TOON-parsing code path "in case" - remove it entirely, matching the full-replacement decision.

  **Parallelization**: Can Parallel: YES | Wave 6 | Blocks: F1 | Blocked By: 24, 32

  **References** (exact sites, verified via grep - excludes `:214,223` which are Task 27's evidence/log-path lines):
  - Header/contract comments: `/Users/douglasjarquin/github/consigliere/bin/cs-crew-state.sh:12,17,26,28,74`.
  - Bounded-call and TOON-parsing comments/logic: `:150,154,165,167,243,251,264`.
  - Library sourcing (coordinate with Task 32): `:65` (`source bin/cs-nm-run-lib.sh` -> `source bin/cs-made-run-lib.sh`).
  - Binary-presence guard: `:293,295` (`command -v no-mistakes` -> `command -v made`).
  - JSON schema to parse against: `made status --json` output defined in Task 21.

  **Acceptance Criteria**:
  - [ ] Shell test suite passes: a mocked `made status --json` response is correctly parsed into the same downstream fields `cs-crew-state.sh` previously extracted from TOON.
  - [ ] `grep -n "TOON\|no-mistakes" bin/cs-crew-state.sh` returns zero matches once combined with Task 27's lines 214/223.

  **QA Scenarios**:
  ```
  Scenario: JSON parsing produces equivalent fields
    Tool: bash
    Steps: Feed a sample `made status --json` payload through the updated parsing function.
    Expected: Output fields match what downstream consigliere logic expects (run state, step name).
    Evidence: evidence/task-26-json-parsing-equivalence.txt
  ```

  **Commit**: YES | Message: `fix(consigliere): parse made JSON status instead of no-mistakes TOON output` | Files: `bin/cs-crew-state.sh` (consigliere repo)

- [x] 27. Log path migration (~/.no-mistakes/logs -> made's evidence store)

  **What to do**: In consigliere, update `bin/cs-crew-state.sh:223` and any other reference to `~/.no-mistakes/logs/*/ci.log` to instead read from made's evidence store (Task 8) - either the in-repo `.made/evidence` path (if `store_in_repo: true` is configured, matching current consigliere usage) or via `made status --json`'s embedded evidence references, whichever the JSON schema from Task 21 makes available. Write a failing shell test first, then implement.
  **Must NOT do**: Do not leave a fallback read path checking `~/.no-mistakes/logs` first - full replacement, single path.

  **Parallelization**: Can Parallel: YES | Wave 6 | Blocks: F1 | Blocked By: 24

  **References**:
  - Exact call site: `/Users/douglasjarquin/github/consigliere/bin/cs-crew-state.sh:223` (`~/.no-mistakes/logs/*/ci.log`).
  - Replacement source: made's evidence store dual-mode behavior (Task 8) and consigliere's existing `store_in_repo: true` / `dir: .no-mistakes/evidence` config in `/Users/douglasjarquin/github/consigliere/.no-mistakes.yaml` (to be replaced by an equivalent `.made.yaml` or renamed config in this same task).

  **Acceptance Criteria**:
  - [ ] Shell test suite passes: log/evidence retrieval returns the correct content from made's evidence path.
  - [ ] `grep -rn "\.no-mistakes/logs\|\.no-mistakes\.yaml" bin/ .no-mistakes.yaml 2>/dev/null` in consigliere returns zero matches (config file itself renamed/replaced).

  **QA Scenarios**:
  ```
  Scenario: Evidence retrieval after migration
    Tool: bash
    Steps: Run a made validation with store_in_repo mode, then call the updated consigliere function to fetch its CI log equivalent.
    Expected: Correct log content returned from the new path.
    Evidence: evidence/task-27-evidence-retrieval.txt
  ```

  **Commit**: YES | Message: `fix(consigliere): read made evidence store instead of no-mistakes log path` | Files: `bin/cs-crew-state.sh`, `.made.yaml` (renamed from `.no-mistakes.yaml`, consigliere repo)

- [x] 28. Busy-detection migration (log-scrape -> socket status query)

  **What to do**: In consigliere, update `bin/cs-watch.sh` (which currently detects a "busy" no-mistakes step by scraping logs) to instead query `made status --json` via the Task 24 shim for the current running stage - a strictly more reliable signal than log-scraping, since made's socket API provides structured state directly. This includes both the logic sites and the descriptive comments referencing the old behavior. Write a failing shell test first (simulate a running stage, assert busy-detection correctly reports it via the new method), then implement.
  **Must NOT do**: Do not keep the log-scraping path as a fallback if the socket query fails - if made's daemon is unreachable, busy-detection should report "unknown/unreachable" explicitly, not silently degrade to scraping stale logs.

  **Parallelization**: Can Parallel: YES | Wave 6 | Blocks: F1 | Blocked By: 24

  **References** (exact sites, verified via grep):
  - Logic sites: `/Users/douglasjarquin/github/consigliere/bin/cs-watch.sh:147`, `:1433`.
  - Descriptive comments needing the same update: `:7`, `:21`, `:1597`.

  **Acceptance Criteria**:
  - [ ] Shell test suite passes: busy-detection correctly reports running/idle state using `made status --json`.
  - [ ] `grep -n "no-mistakes" bin/cs-watch.sh` returns zero matches.
  - [ ] With made's daemon unreachable, busy-detection reports "unreachable" explicitly rather than a stale/incorrect state.

  **QA Scenarios**:
  ```
  Scenario: Busy state detected via socket query
    Tool: bash
    Steps: Start a long-running validation, run the updated busy-detection function mid-run.
    Expected: Reports busy, matching the currently executing stage name.
    Evidence: evidence/task-28-busy-detected.txt

  Scenario: Unreachable daemon reported explicitly
    Tool: bash
    Steps: Stop made's daemon, run busy-detection.
    Expected: Reports "unreachable", not a false idle/busy state.
    Evidence: evidence/task-28-unreachable-reported.txt
  ```

  **Commit**: YES | Message: `fix(consigliere): busy-detection via made socket query instead of log scraping` | Files: `bin/cs-watch.sh` (consigliere repo)

- [x] 29. --mode wiring + mode-semantics migration (no-mistakes -> made delivery mode)

  **What to do**: In consigliere, rename the `no-mistakes` delivery mode to `made` across every mode-semantics site (not just the dispatch table), ensuring the merge-authority constraint (`--mode` remains sole owner of push/PR/merge behavior, per `plans/consigliere-lazy-harness-integration.md:23,:57-58`) is preserved - made's own Push/PR stages (Tasks 17-18) never merge, and `--mode made` in consigliere is the only place a human-approved merge decision is executed. This is a corrected, widened version of the original Task 29: Momus's plan review found the original had no concrete citation and a Metis-caught grep showed the real mode-semantics surface spans nine files, not one. Write a failing shell test first (invoking `--mode made` triggers the expected made CLI calls in the right order via the Task 24 shim, and no merge call is made without explicit human approval recorded), then implement across every site listed below.
  **Must NOT do**: Do not keep `no-mistakes` as a deprecated-but-working mode alias anywhere in this list - full replacement, remove every occurrence.

  **Parallelization**: Can Parallel: YES | Wave 6 | Blocks: F1 | Blocked By: 24

  **References** (exact sites, verified against the real repo via `grep -rn "no-mistakes" bin/`):
  - Merge-authority constraint (must not regress): `/Users/douglasjarquin/github/consigliere/plans/consigliere-lazy-harness-integration.md:23`, `:57-58`.
  - Mode enum and rank dispatch: `/Users/douglasjarquin/github/consigliere/bin/cs-delivery-lib.sh:21` (`CS_DELIVERY_MODES='no-mistakes|direct-PR|local-only'`), `:30` (mode validation case), `:39` (rank comment), `:44` (`no-mistakes) printf '3\n' ;;`).
  - Project-mode registry and defaults: `/Users/douglasjarquin/github/consigliere/bin/cs-project-mode.sh:5,14,17,22,32,58,59,66,81,82,89,90` (every `no-mistakes` string in mode descriptions, defaults, and the closed-set validation).
  - Mode-descriptive prose (not skill invocation - see Task 30 for that): `/Users/douglasjarquin/github/consigliere/bin/cs-brief.sh:9` (usage line), `:63` (mode-transition comment), `:386-387` and `:599-600` (shared-daemon restart rule), `:426` (direct-PR/no-mistakes-only comment), `:456` (direct-PR ships-without-pipeline prose), `:467` (merge-authority prose), `:482` (mode dispatch `*)` case), `:533` (`elif [ "$MODE" = no-mistakes ]`), `:538-540` (PR-ownership prose).
  - Mode-gated checks in other scripts: `/Users/douglasjarquin/github/consigliere/bin/cs-board-capacity.sh:15,149`; `/Users/douglasjarquin/github/consigliere/bin/cs-pr-check.sh:79`; `/Users/douglasjarquin/github/consigliere/bin/cs-promote.sh:15` (usage string); `/Users/douglasjarquin/github/consigliere/bin/cs-spawn.sh:4` (usage string); `/Users/douglasjarquin/github/consigliere/bin/cs-fleet-sync.sh:311` (mode_line fallback default); `/Users/douglasjarquin/github/consigliere/bin/cs-home-seed.sh:443` (capo-routing error message), `:473` (mode gate before remote seeding - distinct from Task 25's remote-seed lines 475/479).

  **Acceptance Criteria**:
  - [ ] Shell test suite passes: `--mode made` dispatches through `cs-made-lib.sh` correctly at every site listed above.
  - [ ] `grep -rln "no-mistakes" bin/cs-delivery-lib.sh bin/cs-project-mode.sh bin/cs-board-capacity.sh bin/cs-pr-check.sh bin/cs-promote.sh bin/cs-spawn.sh bin/cs-fleet-sync.sh` in consigliere returns zero matches.
  - [ ] No code path under `--mode made` calls a merge operation without a preceding explicit human-approval record.

  **QA Scenarios**:
  ```
  Scenario: Mode dispatch correctness
    Tool: bash
    Steps: Invoke consigliere's delivery flow with `--mode made` against a scratch project.
    Expected: made CLI is invoked via the shim in the correct sequence (init, push, status polling); cs-project-mode.sh reports "made" not "no-mistakes" for the project.
    Evidence: evidence/task-29-mode-dispatch.txt

  Scenario: No unauthorized merge
    Tool: bash
    Steps: Run the full `--mode made` flow without providing explicit merge approval.
    Expected: PR remains open, no merge occurs.
    Evidence: evidence/task-29-no-unauthorized-merge.txt
  ```

  **Commit**: YES | Message: `fix(consigliere): rename no-mistakes delivery mode to made across all mode-semantics sites` | Files: `bin/cs-delivery-lib.sh`, `bin/cs-project-mode.sh`, `bin/cs-brief.sh`, `bin/cs-board-capacity.sh`, `bin/cs-pr-check.sh`, `bin/cs-promote.sh`, `bin/cs-spawn.sh`, `bin/cs-fleet-sync.sh`, `bin/cs-home-seed.sh` (consigliere repo)

- [x] 30. Skill string migration ($no-mistakes -> $made)

  **What to do**: In consigliere, update the hardcoded `\$no-mistakes` skill-invocation string and its surrounding direct-to-soldier operational instructions in `bin/cs-brief.sh` to `\$made`. Momus's plan review found the original citation (`:382,387,396`) was wrong - those lines are a `needs-decision` instruction, the shared-daemon restart rule, and an `EOF` marker, not the skill string. The real sites, verified via grep, are `:484` ("no-mistakes doctor"/"no-mistakes init" instruction), `:491` (`\$no-mistakes` invocation), `:496` ("no-mistakes owns the PR object" prose), `:498` ("You drive no-mistakes by responding to its gates"), `:499` (`\$no-mistakes` invocation + `no-mistakes axi run --help` reference), `:505` (`no-mistakes axi respond` instruction), `:508` (`\$no-mistakes reports CI green` instruction). Write a failing shell test first (asserting all seven sites reference `made`/`\$made`, not `no-mistakes`/`\$no-mistakes`), then implement.
  **Must NOT do**: Do not stop at the two `\$no-mistakes` invocation sites (`:491`, `:499`) - the surrounding operational prose at `:484,496,498,505,508` names the tool directly in instructions the soldier agent reads and must also change, or the brief will contradict itself (telling the soldier to invoke `$made` while describing "no-mistakes" behavior in the same paragraph).

  **Parallelization**: Can Parallel: YES | Wave 6 | Blocks: F1 | Blocked By: 24

  **References**:
  - Exact call sites (re-verified against the real file, not estimated): `/Users/douglasjarquin/github/consigliere/bin/cs-brief.sh:484`, `:491`, `:496`, `:498`, `:499`, `:505`, `:508`.

  **Acceptance Criteria**:
  - [ ] Shell test suite passes: all seven sites reference `made`/`\$made`.
  - [ ] `grep -n "no-mistakes" bin/cs-brief.sh` returns zero matches for these seven lines (Task 29 independently covers the mode-semantics lines in the same file, so the file overall reaches zero once both tasks land).

  **QA Scenarios**:
  ```
  Scenario: Brief string updated across all harness variants
    Tool: bash
    Steps: Generate a brief for each harness variant (codex, claude, and any third variant present), inspect the skill invocation string and surrounding instructions.
    Expected: All show `$made` and made-specific operational prose, none show `$no-mistakes` or no-mistakes-specific prose.
    Evidence: evidence/task-30-skill-string-updated.txt
  ```

  **Commit**: YES | Message: `fix(consigliere): update hardcoded skill string and operational prose from $no-mistakes to $made` | Files: `bin/cs-brief.sh` (consigliere repo)

- [x] 31. init/doctor + MADE_DOWN probe migration

  **What to do**: In consigliere, replace `no-mistakes init`/`doctor` bootstrap calls (`bin/cs-home-seed.sh:486` and related) with `made gate init`/`made doctor` equivalents, and add a `MADE_DOWN`-style probe function in the bootstrap flow mirroring the existing `HERDR_DOWN` convention at `bin/cs-bootstrap.sh:171-185`, so consigliere can detect and report an unreachable made daemon the same way it already handles an unreachable herdr server. Write a failing shell test first (simulate made daemon down during bootstrap, assert the `MADE_DOWN` probe fires and bootstrap reports it clearly rather than failing opaquely), then implement.
  **Must NOT do**: Do not let bootstrap fail silently or hang if made's daemon isn't running - it must report the same clear, actionable message style as the existing `HERDR_DOWN` probe.

  **Parallelization**: Can Parallel: YES | Wave 6 | Blocks: F1 | Blocked By: 24

  **References**:
  - `HERDR_DOWN` probe convention to mirror: `/Users/douglasjarquin/github/consigliere/bin/cs-bootstrap.sh:171-185`.
  - Init/doctor call sites to replace: `/Users/douglasjarquin/github/consigliere/bin/cs-home-seed.sh:486`.

  **Acceptance Criteria**:
  - [ ] Shell test suite passes: `MADE_DOWN` probe correctly detects an unreachable made daemon during bootstrap.
  - [ ] `grep -n "no-mistakes" bin/cs-home-seed.sh bin/cs-bootstrap.sh` returns zero matches.
  - [ ] Bootstrap with made daemon down reports a clear, actionable message (not a hang, not an opaque generic failure).

  **QA Scenarios**:
  ```
  Scenario: MADE_DOWN detected during bootstrap
    Tool: bash
    Steps: Ensure made's daemon is not running, run consigliere's bootstrap flow.
    Expected: Bootstrap reports MADE_DOWN clearly and exits/handles gracefully, mirroring HERDR_DOWN behavior.
    Evidence: evidence/task-31-made-down-detected.txt

  Scenario: Successful bootstrap with made up
    Tool: bash
    Steps: Start made's daemon, run bootstrap.
    Expected: `made gate init`/`made doctor` succeed as part of bootstrap, no MADE_DOWN triggered.
    Evidence: evidence/task-31-successful-bootstrap.txt
  ```

  **Commit**: YES | Message: `fix(consigliere): migrate init/doctor bootstrap to made, add MADE_DOWN probe` | Files: `bin/cs-home-seed.sh`, `bin/cs-bootstrap.sh` (consigliere repo)

- [x] 32. cs-nm-run-lib.sh -> cs-made-run-lib.sh rename

  **What to do**: In consigliere, rename `bin/cs-nm-run-lib.sh` to `bin/cs-made-run-lib.sh` in its entirety - this is a whole run-attribution library Momus's plan review found uncovered by any task in the original draft. Rename every `cs_nm_*` function to `cs_made_*` throughout - not just `cs_nm_run`/`cs_nm_trim`, but also `cs_nm_field`, `cs_nm_has_gate`, `cs_nm_strip_quotes`, `cs_nm_gate_*`, `cs_nm_runs_status_for_branch`, `cs_nm_head_matches_worktree`, and `cs_nm_run_is_gate_parked` (Momus round 2 confirmed all of these are called from `cs-crew-state.sh:172-179,270,279-283,356`), and rewrite the internal calls that currently shell the `no-mistakes` binary directly to instead call through `cs-made-lib.sh` (Task 24). Write a failing shell test first (asserting `cs_made_run` exists, `cs_nm_run` does not, and the function correctly wraps a bounded `made` call), then implement.
  **Must NOT do**: Do not leave `cs-nm-run-lib.sh` in place as a compatibility shim that re-sources the new file - delete it, since this is a full replacement.

  **Parallelization**: Can Parallel: YES | Wave 6 | Blocks: 26, F1 | Blocked By: 24

  **References** (exact sites, verified via grep):
  - File header and purpose comment: `/Users/douglasjarquin/github/consigliere/bin/cs-nm-run-lib.sh:2,6,27,33`.
  - Bounded-call function to rewrite: `:40` (`cs_run_timed "$timeout" no-mistakes "$@"`), `:43` (comment).
  - Cross-branch attribution functions to rename/rewrite: `:156`, `:193`.
  - Caller to update in the same task (coordinate with Task 26): `/Users/douglasjarquin/github/consigliere/bin/cs-crew-state.sh:65` (source line), function-call sites elsewhere in that file that invoke `cs_nm_run`/`cs_nm_trim`.

  **Acceptance Criteria**:
  - [ ] Shell test suite passes: `cs_made_run`/`cs_made_trim` exist and behave equivalently to the old `cs_nm_run`/`cs_nm_trim` against a mocked `made` CLI.
  - [ ] `test -f bin/cs-nm-run-lib.sh` fails (file removed); `test -f bin/cs-made-run-lib.sh` succeeds.
  - [ ] `grep -rn "cs_nm_run\|cs_nm_trim\|cs_nm_field\|cs_nm_has_gate\|cs_nm_strip_quotes\|cs_nm_gate_\|cs_nm_runs_status_for_branch\|cs_nm_head_matches_worktree\|cs_nm_run_is_gate_parked\|cs-nm-run-lib" bin/` in consigliere returns zero matches (Momus round 2 found `cs-crew-state.sh:172-179,270,279-283,356` call additional `cs_nm_*` functions beyond `cs_nm_run`/`cs_nm_trim` - all must be renamed `cs_made_*` as part of this task, not just the two originally named).

  **QA Scenarios**:
  ```
  Scenario: Renamed function behaves equivalently
    Tool: bash
    Steps: Call `cs_made_run` against a scratch worktree with a mocked `made` CLI, compare output shape to the old `cs_nm_run` behavior documented in the original file's comments.
    Expected: Same bounded-timeout, stdout-only contract preserved.
    Evidence: evidence/task-32-renamed-function-equivalence.txt

  Scenario: Old file fully removed
    Tool: bash
    Steps: `git status`/`ls bin/cs-nm-run-lib.sh`.
    Expected: File absent, no compatibility shim left behind.
    Evidence: evidence/task-32-old-file-removed.txt
  ```

  **Commit**: YES | Message: `fix(consigliere): rename cs-nm-run-lib.sh to cs-made-run-lib.sh` | Files: `bin/cs-made-run-lib.sh` (new), `bin/cs-nm-run-lib.sh` (removed), `bin/cs-crew-state.sh` (consigliere repo)

- [x] 33. cs-teardown.sh migration

  **What to do**: In consigliere, migrate `bin/cs-teardown.sh`'s run-conclusion-at-teardown logic from calling `no-mistakes axi abort` directly to calling a new `cs_made_abort` function added to `cs-made-lib.sh` (Task 24). Update the `MODE` default, the `CS_NM_CONCLUDE_FAILURE` variable (rename to `CS_MADE_CONCLUDE_FAILURE`) and all its message strings, and the surrounding comments. Write a failing shell test first (simulate a parked made run at teardown, assert the abort call goes through `cs_made_abort` and the correct warning/refusal messages are produced), then implement.
  **Must NOT do**: Do not change the safety semantics - a task with an unconfirmed running validation must still block or warn on teardown exactly as it does today, just against made instead of no-mistakes.

  **Parallelization**: Can Parallel: YES | Wave 6 | Blocks: F1 | Blocked By: 24

  **References** (exact sites, verified via grep):
  - Header comment: `/Users/douglasjarquin/github/consigliere/bin/cs-teardown.sh:43`.
  - Mode default: `:111` (`MODE=$(cs_meta_get "$META" mode || echo no-mistakes)`).
  - Timeout comment: `:120`.
  - Conclude-run logic block: `:397,409,423,431,442,446,454,458,462`.
  - Direct abort call to replace: `:463` (`( cd "$WT" && no-mistakes axi abort ) >/dev/null 2>&1 || true`).
  - Warning/refusal messages: `:483,485,487,567,570,572`.
  - Final comment: `:865`.

  **Acceptance Criteria**:
  - [ ] Shell test suite passes: teardown correctly calls `cs_made_abort` and produces equivalent warning/refusal behavior.
  - [ ] `grep -n "no-mistakes\|CS_NM_CONCLUDE_FAILURE" bin/cs-teardown.sh` returns zero matches.
  - [ ] A simulated unconfirmed-abort scenario still produces a `REFUSED:` message blocking teardown, matching current safety behavior.

  **QA Scenarios**:
  ```
  Scenario: Clean abort at teardown
    Tool: bash
    Steps: Start a made run, initiate teardown for that task before the run completes.
    Expected: cs_made_abort called, run concluded, teardown proceeds.
    Evidence: evidence/task-33-clean-abort.txt

  Scenario: Unconfirmed abort blocks teardown
    Tool: bash
    Steps: Simulate the made daemon not responding to the abort request during teardown.
    Expected: Teardown reports REFUSED (or WARNING with --force), matching today's safety behavior.
    Evidence: evidence/task-33-unconfirmed-abort-blocks.txt
  ```

  **Commit**: YES | Message: `fix(consigliere): migrate teardown run-conclusion from no-mistakes to made` | Files: `bin/cs-teardown.sh`, `bin/cs-made-lib.sh` (consigliere repo)

- [x] 34. cs-classify-lib.sh comment/logic migration

  **What to do**: In consigliere, update `bin/cs-classify-lib.sh`'s comments describing busy-state classification - these describe the exact "actively-running no-mistakes step" behavior that Task 28 changes to query made's socket API instead of scraping logs, so leaving them unmigrated would leave the codebase's own documentation wrong about its current behavior. Write a failing shell test first (a doc-consistency check asserting no `no-mistakes`-referencing comment remains once Task 28 lands), then implement.
  **Must NOT do**: Do not change this file's actual classification logic beyond what Task 28 already changed in `cs-crew-state.sh`/`cs-watch.sh` - this task is about keeping `cs-classify-lib.sh`'s own comments and any direct references consistent with that change, not re-deriving busy-detection a second time.

  **Parallelization**: Can Parallel: YES | Wave 6 | Blocks: F1 | Blocked By: 24, 28

  **References** (exact sites, verified via grep):
  - `/Users/douglasjarquin/github/consigliere/bin/cs-classify-lib.sh:15` (reuses `cs-crew-state.sh`, "may make a bounded no-mistakes call").
  - `:43` ("worktree or no-mistakes install").
  - `:91` ("A no-mistakes soldier appends...").
  - `:391` ("an actively-running no-mistakes step (running/fixing/ci)").
  - `:399` ("cs-crew-state.sh may make a bounded no-mistakes call").

  **Acceptance Criteria**:
  - [ ] `grep -n "no-mistakes" bin/cs-classify-lib.sh` returns zero matches.
  - [ ] The updated comments accurately describe made-based busy detection (reviewed for correctness, not just string-replaced).

  **QA Scenarios**:
  ```
  Scenario: Comment accuracy after migration
    Tool: bash
    Steps: Read the updated comments alongside Task 28's actual implementation in cs-watch.sh/cs-crew-state.sh.
    Expected: Comments correctly describe the made-socket-query-based behavior, no stale references.
    Evidence: evidence/task-34-comment-accuracy.md
  ```

  **Commit**: YES | Message: `docs(consigliere): update cs-classify-lib.sh comments for made migration` | Files: `bin/cs-classify-lib.sh` (consigliere repo)

- [x] 35. Long-tail sweep (cs-deps-lib.sh, cs-harness-lib.sh, cs-sessionstart-run.sh, cs-update.sh)

  **What to do**: In consigliere, migrate the remaining four files with real `no-mistakes` references: `bin/cs-deps-lib.sh` (dependency-check listing and install instructions - update the required-binary name and the install-source URL from `https://github.com/kunchenguid/no-mistakes` to made's own repo URL), `bin/cs-harness-lib.sh` (evidence-path example comment `.no-mistakes/evidence/...` -> `.made/evidence/...`), `bin/cs-sessionstart-run.sh` (comment about gate agents never starting a fleet session - update the tool name), and `bin/cs-update.sh` (comment listing `.no-mistakes/` among operational dirs preserved across updates - update to `.made/`). Write a failing shell test first for the dependency-check function (asserting it checks for `made`, not `no-mistakes`, and reports the correct install URL), then implement all four files.
  **Must NOT do**: Do not skip any of the four files on the assumption that "it's just a comment" - Task 34's rationale applies equally here: stale tool-name references in comments and user-facing dependency messages are real correctness defects post-migration.

  **Parallelization**: Can Parallel: YES | Wave 6 | Blocks: F1 | Blocked By: 24

  **References** (exact sites, verified via grep):
  - `/Users/douglasjarquin/github/consigliere/bin/cs-deps-lib.sh:84` (dependency list), `:108` (description string), `:130` (install-source URL).
  - `/Users/douglasjarquin/github/consigliere/bin/cs-harness-lib.sh:681` (evidence-path example comment).
  - `/Users/douglasjarquin/github/consigliere/bin/cs-sessionstart-run.sh:42` (gate-agent comment).
  - `/Users/douglasjarquin/github/consigliere/bin/cs-update.sh:10` (operational-dirs comment).

  **Acceptance Criteria**:
  - [ ] `grep -n "no-mistakes" bin/cs-deps-lib.sh bin/cs-harness-lib.sh bin/cs-sessionstart-run.sh bin/cs-update.sh` returns zero matches.
  - [ ] The dependency-check function correctly reports `made` as missing (not `no-mistakes`) when `made` is not installed, with the corrected install URL.

  **QA Scenarios**:
  ```
  Scenario: Dependency check reports made
    Tool: bash
    Steps: Uninstall/rename the `made` binary temporarily, run consigliere's dependency check.
    Expected: Reports `made` missing with the correct install instructions, no reference to no-mistakes.
    Evidence: evidence/task-35-dependency-check.txt
  ```

  **Commit**: YES | Message: `fix(consigliere): migrate remaining no-mistakes references in deps/harness/sessionstart/update scripts` | Files: `bin/cs-deps-lib.sh`, `bin/cs-harness-lib.sh`, `bin/cs-sessionstart-run.sh`, `bin/cs-update.sh` (consigliere repo)

## Final Verification Wave (MANDATORY - after ALL implementation tasks)
> ALL must APPROVE. Present consolidated results to user and get explicit "okay" before completing.
- [x] F1. Plan Compliance Audit

  **What to do**: Verify every Must Have is present and every Must NOT Have is absent. Re-run the Definition of Done commands from the Work Objectives section and confirm each passes. Confirm no task was skipped or silently descoped.
  **Must NOT do**: Approve based on partial evidence or task self-reports alone - re-execute the commands.

  **Acceptance Criteria**:
  - [ ] `cd made && go build ./... && go test ./...` exits 0.
  - [ ] `grep -rn "no-mistakes" /Users/douglasjarquin/github/consigliere/bin/` returns zero matches (all 17 files, comments included - see Context item 8 and Tasks 25-35).
  - [ ] `grep -rln "gitlab\|bitbucket\|azuredevops\|opencode" made/internal/` returns zero matches.

  **QA Scenarios**:
  ```
  Scenario: Full pipeline end to end
    Tool: bash
    Steps: In a scratch git repo, run `made gate init`, make a trivial passing commit, `git push made <branch>`.
    Expected: All 9 stages report success in `made status --json`; a PR is opened via `gh pr view --json state` showing OPEN; no merge occurred.
    Evidence: evidence/task-F1-full-pipeline.txt

  Scenario: Failing test blocks push
    Tool: bash
    Steps: Same scratch repo, introduce a failing test, `git push made <branch>`.
    Expected: Non-zero exit, clear message naming the Test stage, real remote receives no new ref (`git ls-remote origin` unchanged).
    Evidence: evidence/task-F1-failing-test-block.txt
  ```

  **RESOLVED via plans/made-orchestrator.md**: at the time this plan's own tasks completed, no task wired the 9 stages into a real run, so this F1 could not pass - that gap became made-orchestrator.md's entire reason to exist. made-orchestrator.md's own F1 re-ran these exact two QA scenarios for real (same evidence file paths, now populated) against a real `made` binary, a real daemon, and real generated git hooks - full pass and failing-test-halt both proven live. One refinement: "all 9 stages report success" now means the run's final `state` is `running` (not `completed`) with all 9 `stages[].result == "pass"`, since a CI-passed run stays open pending human merge (a deliberate design decision made during that plan, not a shortfall) - PR-opened-not-merged still holds exactly as stated here.

  **PARTIAL, same caveat as F3**: the second acceptance criterion (zero `no-mistakes` matches in `consigliere/bin/`) holds only on consigliere's `made-migration` branch (PR #76, open). It cannot hold on consigliere's main until that PR merges. The made-side criteria (build/test, forbidden-SCM grep) are fully proven.

  **Commit**: NO

- [x] F2. Code Quality Review

  **What to do**: Run `golangci-lint run ./...` across the made module and `shellcheck` across all new/edited consigliere bin/ scripts. Review diffs for dead code, TODOs left in, or Must-NOT-Have violations.
  **Must NOT do**: Silence lint findings with blanket `//nolint` comments - fix or justify each one explicitly.

  **Acceptance Criteria**:
  - [ ] `golangci-lint run ./...` (from made/) reports zero issues.
  - [ ] `shellcheck bin/cs-made-lib.sh bin/cs-home-seed.sh bin/cs-crew-state.sh bin/cs-watch.sh bin/cs-brief.sh bin/cs-bootstrap.sh` (from consigliere/) reports zero issues.

  **QA Scenarios**:
  ```
  Scenario: Lint clean run
    Tool: bash
    Steps: Run both lint commands above.
    Expected: Zero findings from both tools.
    Evidence: evidence/task-F2-lint-clean.txt
  ```

  **Commit**: NO

- [ ] F3. Real Manual QA

  **What to do**: Actually run the herdr fail-open scenario and the consigliere `--mode made` delivery path against a live scratch project, as a human/agent user would.
  **Must NOT do**: Substitute unit test output for this - it must be a real invocation.

  **Acceptance Criteria**:
  - [ ] With `herdr server` running, a gate run shows a pane via `herdr pane list --session made`.
  - [ ] With herdr stopped (`pkill -f herdr` in a disposable environment, or simply not starting it), the identical gate run still completes successfully.
  - [ ] `consigliere`'s soldier flow, invoked with `--mode made` against a scratch project, completes a full delegate-validate-PR cycle with no reference to `no-mistakes` anywhere in its logs.

  **QA Scenarios**:
  ```
  Scenario: herdr up
    Tool: bash
    Steps: Start herdr server, run a gate validation, list panes.
    Expected: Pane visible, tailing live command output.
    Evidence: evidence/task-F3-herdr-up.txt

  Scenario: herdr down
    Tool: bash
    Steps: Ensure no herdr server/socket is reachable, run the same gate validation.
    Expected: Gate run completes successfully; log shows a fail-open warning, not an error.
    Evidence: evidence/task-F3-herdr-down.txt
  ```

  **PARTIALLY RESOLVED via plans/made-orchestrator.md, still open**: that plan's own F3 proved the herdr up/herdr-down fail-open pair for real against a genuinely orchestrated run (evidence/task-F3-herdr-fail-open-real.txt) - including against a real, protocol-incompatible live herdr instance found running on the host, which strengthens rather than weakens the proof (a mismatched-but-real herdr still failed open correctly). The consigliere `--mode made` full delegate-validate-PR cycle criterion is STILL NOT PROVEN: consigliere's own migration (PR #76 at github.com/douglasjarquin/consigliere) is still open, unmerged, and its `--mode made` path has never been exercised against a live `made` daemon through consigliere's real bash orchestration. This box stays unchecked until PR #76 merges and a real consigliere-driven run is performed.

  **Commit**: NO

- [x] F4. Scope Fidelity Check

  **What to do**: Re-read this plan's Must Have / Must NOT Have lists against the final diff. Confirm the plan's stated principle - independent synthesis, not a one-to-one copy - actually holds: spot-check that made's implementations of gitgate, exec/reaping, and the socket envelope are structurally distinct from no-mistakes' and herdr's own source (different naming, different file layout, no copied comments or identifiers).
  **Must NOT do**: Treat "it works" as sufficient - the principle explicitly required independent design, not just independent behavior.

  **Acceptance Criteria**:
  - [ ] Side-by-side comparison of `made/internal/gitgate/*.go` against `no-mistakes/internal/git/*.go` shows independent structure (documented in evidence, not just asserted).
  - [ ] Side-by-side comparison of `made/internal/api/*.go` against `herdr/src/api/schema.rs` shows idiomatic similarity (method/params/id shape) without wire-format copying.

  **QA Scenarios**:
  ```
  Scenario: Independence spot-check
    Tool: bash
    Steps: Diff file structures and skim implementations side by side; note any suspiciously identical logic.
    Expected: No copied code found; design is recognizably made's own.
    Evidence: evidence/task-F4-independence-check.md
  ```

  **Commit**: NO

## Commit Strategy
Each implementation task (1-35) commits independently once its own acceptance criteria and QA scenarios pass, using Conventional Commits (`feat(gitgate): ...`, `feat(pipeline): ...`, `fix(consigliere): ...` for cross-repo migration tasks committed in the consigliere repo). Final Verification Wave tasks (F1-F4) do not commit - they are read-only audits of what waves 1-6 already committed. No task should be committed with failing tests or unresolved lint findings.

**Deviation (disclosed)**: tasks 1-23 were executed before any commit was made, then committed by SUBSYSTEM in one batch (`393413e`..`93004ff` plus plan/state/evidence commits) rather than per-task, per an explicit mid-session decision ("commit now, then per-task going forward"). Additionally `745f7c7` combined Tasks 21 and 22 into one CLI commit. Consigliere tasks 24-35 were committed per-task on that repo's `made-migration` branch (PR #76). Evidence for tasks 24-35 was later copied into this repo's evidence/ directory, where this plan's Verification Strategy says it belongs.

## Success Criteria
- `made` fully replaces no-mistakes as consigliere's validation backend; no-mistakes is no longer referenced anywhere in consigliere's bin/, skills/, or config.
- The full 9-stage pipeline runs end to end against GitHub with Claude and Codex as the only agent backends.
- herdr provides live pane visibility for every gate run when available, and every gate run succeeds identically when herdr is unavailable.
- The trusted-vs-pushed config boundary is enforced exactly as specified in the Metis Review section, with a failing-then-passing test proving each of the four rules.
- No merge-authority violation is possible in code: made's PR stage has no code path that calls a merge API.

## Made remediation continuation from exact base `3e19ed9d598a68149da5a73949533e8095ca4403`

This linked section is the canonical ledger for the continuation work.
Historical task claims above remain unchanged.

### Phase 4A - contract and durability gates

- [x] Public structured contract: exact `capabilities --json`, `run.submit`, `run.status`, `run.list`, `run.cancel`, `review.decide`, and `doctor --json` surfaces are implemented with exact run IDs and no global-latest status fallback.

  **References**: `cmd/made/capabilities.go`, `cmd/made/run.go`, `cmd/made/run_handlers.go`, `cmd/made/status.go`, `cmd/made/doctor.go`, and `evidence/phase-1-red-made-remediation-continuation.md`.

  **Acceptance Criteria**: The obsolete `made status` command rejects with exit code 2; exact-ID status returns structured lifecycle state; missing IDs fail closed; capabilities lists the supported commands.

  **QA Scenarios**: Run the real binary against a disposable Made home, submit a disposable identity, query the exact run ID, query an unknown ID, and invoke the obsolete status command.

  **Evidence**: `evidence/phase-4-manual-qa.md`.

- [x] Lifecycle and durability: queued identity, submission refresh, exact input/output SHA and submission metadata, queued cancellation, awaiting-merge, succeeded/canceled/superseded terminal states, first-wins decisions, restart recovery, torn-tail tolerance, durable ordering, and bounded WAL retention are implemented.

  **References**: `internal/daemon/runmanager.go`, `internal/daemon/persistence.go`, `internal/daemon/runstate.go`, `internal/daemon/reviewdecisions.go`, and `evidence/phase-3-lifecycle-durability.md`.

  **Acceptance Criteria**: A submitted record is durable before queue drain; a daemon restart restores the exact record without replaying unrelated work; awaiting-merge remains non-terminal until succeeded; a queued cancel performs no work; torn final records are ignored and non-final corruption fails closed.

  **QA Scenarios**: Run the daemon tests for queued cancellation, awaiting-merge, restart, torn-tail, retention, decision conflict, and the real binary restart scenario.

  **Evidence**: `internal/daemon/persistence_contract_test.go`, `evidence/phase-3-lifecycle-durability.md`, and `evidence/phase-4-manual-qa.md`.

- [x] Evidence and reviewer containment: atomic in-repository evidence writes, compare-and-swap orphan publication, path containment, stage evidence references, and patch-only auto-fix commits are enforced.

  **References**: `internal/evidence/inrepo.go`, `internal/evidence/orphan.go`, `internal/orchestrator/workfunc.go`, `internal/pipeline/review/review.go`, and `evidence/phase-3-lifecycle-durability.md`.

  **Acceptance Criteria**: Concurrent orphan writers retain every run; evidence files use durable write ordering; an auto-fix never stages unrelated worktree files; stage and run snapshots preserve evidence references.

  **QA Scenarios**: Run the evidence concurrency suite and reviewer containment scenario with an unrelated disposable file present.

  **Evidence**: `internal/evidence/evidence_contract_test.go`, `internal/pipeline/review/review_contract_test.go`, and `evidence/phase-3-lifecycle-durability.md`.

- [x] Semantic configuration and enforced switches: unknown or multiple YAML documents fail closed, trusted configuration remains authoritative, and the trusted `no_ci` switch is enforced by the orchestrator.

  **References**: `internal/config/config.go`, `internal/config/config_contract_test.go`, `internal/orchestrator/workfunc.go`, and `evidence/phase-3-lifecycle-durability.md`.

  **Acceptance Criteria**: Unknown semantic switches are rejected; pushed configuration cannot override trusted execution settings without the existing explicit trust switch; `no_ci` records a skipped CI stage instead of invoking CI.

  **QA Scenarios**: Load disposable trusted/pushed YAML fixtures with unknown fields and run a trusted `no_ci` stage fixture.

  **Evidence**: `internal/config/config_contract_test.go` and `evidence/phase-3-lifecycle-durability.md`.

### Phase 4B - compatibility and final validation gates

- [x] Strict external compatibility: GitHub uses `gh pr checks --json name,state,bucket,link`, preserves numeric workflow run IDs, exposes authentication/check/log/rerun errors, and Codex uses the structured `exec` task contract while unsupported Claude behavior is rejected explicitly.

  **References**: `internal/github/client.go`, `internal/github/testdata/fakegh/main.go`, `internal/agent/spawn.go`, `internal/agent/testdata/fakeagent/main.go`, and `evidence/phase-2-external-contracts.md`.

  **Acceptance Criteria**: Strict fakes reject obsolete or invented arguments; focused GREEN tests accept only supported GitHub JSON and Codex structured output; PR URLs cannot reach workflow run operations.

  **QA Scenarios**: Run the focused GitHub/CI and agent/review suites against disposable repositories, strict fake boundaries, and process fixtures.

  **Evidence**: `evidence/phase-1-red-made-remediation-continuation.md` and `evidence/phase-2-external-contracts.md`.

- [x] Disposable live scenarios: the real Made binary was exercised only against a disposable Made home, exact run identities, a restart, strict boundary behavior, and the named non-default Herdr lab session.

  **References**: `evidence/phase-0-grounding-made-remediation-continuation.md`, `evidence/phase-4-manual-qa.md`, and the required Herdr helper path in the task brief.

  **Acceptance Criteria**: The live scenario does not initialize a real gate, submit a real project, alter the shared daemon, or use the default Herdr session.

  **QA Scenarios**: Start and stop only the disposable Made daemon, query exact IDs, restart it, and probe the named Herdr session through the helper.

  **Evidence**: `evidence/phase-4-manual-qa.md`.

- [x] Final validation and delivery: run the Made-only build, race/shuffle test, vet, configured lint, changed-file diagnostics, final branch scope review, review-work/runtime audit, direct branch push, and direct PR creation.

  **References**: `evidence/phase-1-red-made-remediation-continuation.md`, `evidence/phase-2-external-contracts.md`, `evidence/phase-3-lifecycle-durability.md`, `evidence/phase-4-manual-qa.md`, `evidence/phase-4-final-validation.md`, `evidence/phase-4-review-audit.md`, and `evidence/phase-4-herdr-cleanup.md`.

  **Acceptance Criteria**: The final commit list starts at the exact base SHA `3e19ed9d598a68149da5a73949533e8095ca4403`; only Made files and linked evidence/plan records are changed; all authorized local validation is green; PR [#2](https://github.com/douglasjarquin/made/pull/2) is open on `cs/made-remediation-continuation`; no default branch push or merge occurs.

  **QA Scenarios**: Execute the final Made-only validation commands, inspect the exact full SHA and changed-file list, perform required review audits, push only the direct branch, and open the direct PR with `gh-axi`.

  **Evidence**: `evidence/phase-4-final-validation.md`, `evidence/phase-4-review-audit.md`, `evidence/phase-4-herdr-cleanup.md`, the final commit list, the branch push receipt, and PR [#2](https://github.com/douglasjarquin/made/pull/2).

**Commit**: YES | Message: `fix(made): complete remediation continuation from exact base` | Files: Made source, Made tests, `plans/made-rewrite.md`, and phase-scoped evidence only.

### Conflict repair continuation receipt - exact final source `918da271aa9521d292bbda22a862591b770f9af6`

- [x] Conflict repair preserved the exact base `3e19ed9d598a68149da5a73949533e8095ca4403`, retained `origin/main` as merge parent `34d44be504291482d973c65bd427ba964df5e0e9`, and removed only obsolete duplicate CLI paths.

  **References**: `evidence/phase-4-conflict-repair.md`, merge commit `0a7c21d6d3001b85b38330766e01980bd5e92f2c`, and final source commits `bac8ed2777f584d98eb1ba8015cf1269d01a8c1e` and `918da271aa9521d292bbda22a862591b770f9af6`.

  **Acceptance Criteria**: `git diff --name-only 3e19ed9d598a68149da5a73949533e8095ca4403..HEAD` remains Made-only; no unmerged paths remain; the merge parents are exact.

  **QA Scenario**: Run `git merge-base HEAD 3e19ed9d598a68149da5a73949533e8095ca4403`, `git rev-parse HEAD^1`, `git rev-parse HEAD^2`, and `git diff --name-only ...`.

  **Evidence**: `evidence/phase-4-conflict-repair.md`.

- [x] Durability review correction preserves the compaction-triggering transition across restart by overlaying the candidate snapshot before WAL truncation.

  **References**: `internal/daemon/persistence.go`, `internal/daemon/persistence_contract_test.go`, and `evidence/phase-4-conflict-repair.md`.

  **Acceptance Criteria**: The new compaction regression is RED against the pre-fix code and GREEN at `918da271aa9521d292bbda22a862591b770f9af6`; the full daemon package remains green.

  **QA Scenario**: Run `go test ./internal/daemon -run '^TestRunManager_CompactionPersistsTriggeringTransition$' -count=1` and the affected package suite with process-local Git configuration.

  **Evidence**: `evidence/phase-4-conflict-repair.md` and `evidence/phase-4-final-validation.md`.

- [x] Final exact-SHA local validation and disposable real-binary QA were rerun after the durability correction.

  **References**: `evidence/phase-4-final-validation.md` and `evidence/phase-4-manual-qa.md`.

  **Acceptance Criteria**: `go test ./... -count=1`, `go test -race -shuffle=on -count=1 ./...`, `go build ./...`, `go vet ./...`, configured `make lint`, and `gofmt` checks exit `0`; the disposable binary observes exact structured boundaries and cleans its local daemon/socket/lock.

  **QA Scenario**: Build the binary from the exact final source, run capabilities, obsolete status, disposable daemon status/list/unknown-ID/stop, then verify cleanup.

  **Evidence**: `evidence/phase-4-manual-qa.md` and `evidence/phase-4-final-validation.md`.

- [x] Fresh review-work/runtime-audit receipts are bound to the final source SHA, and PR #2 is pushed and verified conflict-free.

  **Acceptance Criteria**: All applicable review lanes have terminal verdicts bound to source SHA `12b83a6649b5e198049754f1cb6427d7b0dc51a0`; only `cs/made-remediation-continuation` is pushed; PR #2 head matches the final source branch and reports clean mergeability.

  **QA Scenario**: Run the review audit, inspect the exact commit list and changed-file scope, push only the task branch with `gh-axi`, and read PR #2 metadata without merging.

  **Evidence**: `evidence/phase-4-review-audit.md`, `evidence/phase-4-conflict-repair.md`, the successful hosted check `95537594230`, and the final PR read receipt.

  **Commit**: YES | Message: `fix(made): preserve compaction transition during conflict repair` | Files: `internal/daemon/persistence.go`, `internal/daemon/persistence_contract_test.go`, phase-scoped evidence, and this continuation receipt.
