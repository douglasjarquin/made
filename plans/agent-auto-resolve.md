# Agent auto-resolve: config-driven fallback across review-harness CLIs

## TL;DR
> **Summary**: Replace `made`'s hardcoded single `agent:` with an ordered, probed candidate list (`agent: auto` + `agents: [...]`), a per-run push-option override, mid-run fallback on quota exhaustion, and a structured "every candidate failed" reason instead of a generic error.
> **Deliverables**: config schema + validator fix; `internal/agent/resolve.go` resolver (presence/auth/quota probing); `ErrAgentCapacity` classification in `spawn.go`; fallback retry wired into both existing `AgentKind()` call sites (`workfunc.go:272`, `managed/runner.go:208`); `git push -o agent=<kind>` preference layer (trust-boundary gated); structured `AgentResolution` detail additive to daemon/managed/verify JSON; `made doctor` reporting; new multi-binary fake-CLI test harness; full test suite.
> **Effort**: Large
> **Parallel**: YES - 3 waves
> **Critical Path**: config schema -> resolver -> call-site wiring -> push-option layer -> structured surfacing -> doctor -> tests -> docs

## Context

### Original Request
Boss's own repo. `agent: codex` hardcoded in `.made.yaml` broke a real run last night on a host where codex had no auth. Build config-driven autodetect + fallback + mid-run capacity handling + structured escalation-ready failure, so a caller (consigliere) can eventually recognize "every agent option is exhausted" and get a human involved - that recognition/notification layer is explicitly NOT part of this task.

### Verified Architecture (4 read-only research passes + 1 Metis gap-analysis pass, all against actual source in this worktree)

- `internal/config/config.go:37-38`: `Agent string \`yaml:"agent"\`` + `Agents []string \`yaml:"agents"\`` already exist. `Agents` is validated per-entry (`agent.ParseKind`, lines 231-235) but **never consumed** - confirmed dead field, only 3 non-test call sites of `AgentKind()` exist and none read `c.Agents`.
- `internal/config/config.go:194-199` `AgentKind()`: `agent.ParseKind(c.Agent)`, fails on `""` or unknown value. Existing test (`config_extended_test.go:183-197`) asserts this fails-closed contract - **must not change**; new methods are added alongside it, not in place of it.
- `internal/config/config.go:218-223` `validateReviewRequiresAgent`: `if c.Review.Required && c.Agent == ""` -> hard error. **This is a real contradiction with the goal**: a config with `agents: [claude, codex]`, no `agent:`, `review.required: true` fails `Config.Validate()` before resolution ever runs. Must be relaxed (see Decision D1).
- `internal/config/file.go:28` `LoadEffectiveConfig(trustedPath, pushedPath) (Config, error)` - strict decode (`KnownFields(true)`), `version: 1` enforced for first-class/legacy paths. `Agent`/`Agents` both flow through the same trusted/pushed merge pair (file.go:72-80): pushed values used only when `trusted.AllowRepoCommands == true`, else trusted values win. Called from `internal/orchestrator/scaffold.go:145` inside `resolveConfig` (scaffold.go:128).
- **No new YAML key is being added** - `agent: auto` is a new *value* for an existing string field, `agents:` is an existing-but-dead field gaining real semantics. `KnownFields(true)` strict-decode risk does not apply; no forward/backward-compat wall here.
- **Two independent call sites** currently call `cfg.AgentKind()` expecting one `agent.Kind`, each must be updated: `internal/orchestrator/workfunc.go:272` (`reviewStage()`, daemon-backed pipeline) and `internal/managed/runner.go:208` (only when `ReviewSource == ReviewSourceInternal`).
- `internal/agent/agent.go`: `Kind` enum (`KindCodex/KindClaude/KindCursor/KindGrok`), `SupportedKinds()` fixed order `[codex, claude, cursor, grok]`, `Kind.stateDirs()` per-kind HOME dirs consumed by containment.
- `internal/agent/spawn.go` `SpawnWithEvidence` (~line 46): every launch/exit failure wrapped as one generic `fmt.Errorf` (~lines 89-93) - no classification exists. `ErrAgentCapacity` does not exist anywhere in the package yet.
- `internal/agent/containment.go`: bwrap ro-binds `/`, then binds `Kind.stateDirs()` writable, then applies the candidate-source mask **last** (so it wins). This ordering is load-bearing for the real review spawn; probes (see Decision D8) deliberately bypass this entirely since they touch no candidate worktree content.
- `internal/agent/testdata/fakeagent` + `internal/agent/agenttest/agenttest.go:17-36`: fakes the review-invocation (structured findings) path only, via **one** binary named `fakeagent`, kind selected by the process-wide env var `FAKE_AGENT_KIND`, built once under `sync.Once`. **Structurally cannot express multi-candidate scenarios** (e.g. "codex missing, claude present-but-unauthed, grok quota-exhausted") in one test process, since candidate selection must happen via `exec.LookPath` on real per-kind binary names (`codex`, `claude`, `cursor-agent`, `grok`) on `PATH`, not via one shared env var. **New harness is a Wave-1 prerequisite, not incidental to resolve_test.go.**
- No code anywhere in the repo shells out to `codex login status` / `claude auth status` today - `resolve.go` is first. No confirmed auth-status equivalent exists for `cursor`/`grok` anywhere (repo-wide grep, zero hits) - per brief, never invent one; those two kinds are **presence-only** (Decision D2).
- `quota-axi` (`~/.npm-global/bin/quota-axi`, confirmed present/runnable on this host, `schemaVersion: 5`) `--provider <kind> --json --full --no-credential-refresh` returns `providers[].quotaSemantics.effectiveAvailability[]`, each `{scope, status: "known"|"unknown", effectivePercentRemaining, boundedBy, limitingWindowIds, pace, runway, selection}` - a **pre-computed bound-across-all-relevant-windows** value per scope. Real captured examples (claude: authenticated, one `scope: "all_models"` entry, `effectivePercentRemaining: 28`; codex: unauthenticated, `windows: []`, `quotaSemantics.status: "unknown"`, `effectiveAvailability: []`) are saved as test fixtures (see Task 3).
- Push-option layer verified **clean, low-friction, but with 2 real gaps Metis found** (Decisions D4, D5 below): `internal/gitgate/bare.go`'s `InitBare` (no `receive.advertisePushOptions` set anywhere today) + `internal/gitgate/hook.go`'s generated `postReceiveScript` (no `GIT_PUSH_OPTION_*` read anywhere) + `cmd/made/gate.go:67-77`'s `notify-push` (plain stdlib `flag`, trivial to add a flag) + `daemon.go:372-381`'s `gateNotifyPushParams` (strict-decoded but additive/`omitempty`-safe) + `internal/orchestrator/workfunc.go:47-50`'s `Options` struct (doc comment **already anticipates** "agent binary resolution" as an out-of-band per-run parameter, exact precedent: existing `CandidateOutputSHA` field).
- **Known, accepted gap**: the offline-submission queue path (`queueOfflineGateSubmission`, `internal/daemon/spool.go:16-22` `GateSubmission`, an identity/dedup record, non-strict `json.Unmarshal`, no schema version) does not carry the push-option preference through replay. Accepted as documented limitation (falls back to config resolution on replay) rather than extending the spool's schema - the spool's job is push-identity/dedup, not run parameters (Decision D6).
- Failure-JSON surfacing verified **additive/free on all 3 surfaces**, no schema-version bump needed anywhere, following this repo's own established convention (issue #61 added `ReusedCommands`/`Reused` fields to `managed.StageResult`/`verify.StageReceipt` without bumping `TerminalManifest.SchemaVersion` (3) or `ReceiptSchemaVersion` (3)):
  1. `internal/daemon/runstate.go:8-14` `StageResult{Name,Result,Message,Error string, EvidenceRefs []string}` - flat strings, **unversioned** JSON (no SchemaVersion field at all on this surface).
  2. `internal/managed/contract.go:145-156` `StageResult{Stage,Outcome,Message string,...}` - same additive pattern.
  3. `internal/verify/receipt.go:35-57` `Receipt`/`StageReceipt{Name,Status,Message,Reused}` (own `ReceiptSchemaVersion=3`, does not embed `TerminalManifest` inline) - `BuildReceipt` needs one line to copy the new field across.

### Metis Gap-Analysis Findings and Resolving Decisions

Metis found 4 contradictions, 6 ambiguities, 11 missing constraints, 8 execution risks, 5 topology gaps. This plan resolves every one that changes the implementation; each resolution is recorded here as a **Decision (D#)** so the implementer has zero judgment calls left. Genuinely product-level calls are flagged `[DECISION NEEDED]` in the summary below for the boss to confirm before work starts - there are none load-bearing enough to block starting; everything below is a defensible default with recorded rationale.

- **D1 (resolves contradiction: review.required + agents-only config)**: Relax `validateReviewRequiresAgent` (config.go:218-223) so it only errors when `Review.Required` AND there is genuinely no selection mechanism - i.e. `Agent == ""` (or `"auto"`) is now **always** a valid selection mechanism because it has a well-defined default candidate list (`Agents` if non-empty, else `SupportedKinds()` - never empty). New logic: error only if `Review.Required && Agent != "" && Agent != "auto"` is false AND... actually simpler: **drop this validator's hard-error entirely for the auto/empty case** - auto-detect can never fail static validation, only runtime resolution can fail (and that's the structured-failure path, not a config error). Keep the error only for an explicit-but-unknown `Agent` value (already covered by `validateCommon`'s `ParseKind` call). `AgentKind()` itself is untouched (existing test stays green); the validator change is additive-permissive, not behavior-narrowing.
- **D2 (resolves contradiction: unauthenticated-skip goal unachievable for cursor/grok)**: Explicitly scope the auth-check step to **codex + claude only** (the two kinds with a verified cheap auth-status command). `cursor`/`grok` candidates are **presence-only** (`LookPath` alone, exactly as brief allows) - an installed-but-logged-out `cursor-agent` will be selected and can still fail downstream; this is a documented, intentional limitation, not a bug. Recorded in `Must NOT Have` and in a code comment on the resolver.
- **D3 (resolves contradiction: Outcome enum vs additive field)**: Do **not** add a new `managed.Outcome` enum value (that's a documented exit-code contract change, per `docs/managed-validation-v1.md`, not a free additive field). Reuse existing `OutcomeInfrastructureError` for the all-candidates-exhausted case - from made's exit-code contract perspective, "no working review harness could be resolved" genuinely is an infrastructure problem, consistent with existing `OutcomeInfrastructureError` usage (config-locate failures, etc.), and attach the new structured `AgentResolution` detail as an additive field alongside it.
- **D4 (resolves contradiction: push-option override bypasses the pushed-config trust boundary)**: Gate the push-option agent preference **exactly like pushed config values already are** (file.go:72-80) - only honor `-o agent=<kind>` when the trusted config's `AllowRepoCommands == true`; otherwise ignore it (record in resolution evidence that a preference was present but not honored, per today's config-trust precedent) and fall back to trusted config's own resolution. This closes the security gap Metis found by reusing an existing, already-decided trust rule rather than inventing a new one.
- **D5 (resolves missing constraint: existing-gate migration hazard)**: `git push -o` hard-fails ("the receiving end does not support push options") against a gate whose bare repo predates this change. Fix: don't rely solely on `InitBare` - also defensively (idempotently) set `receive.advertisePushOptions=true` wherever the daemon opens/uses an existing gate repo for a push (e.g. in the `notify-push`/gate-admit handling path, or lazily on first use), so existing gates self-heal with no manual migration step. Cheap, idempotent, always-safe `git config` call.
- **D6 (resolves missing constraint / execution risk: offline-queue drop)**: Already decided above - accepted, but must be an explicit **test case** (assert offline-replay silently drops the preference and falls back to config resolution) plus one doc line, not just a silent decision.
- **D7 (resolves ambiguity: mid-run fallback vs already-landed auto-fix commits)**: Scope the `ErrAgentCapacity` classification and fallback retry to the **primary review-invocation spawn only** (the initial structured-findings call inside `review.Run`/the managed equivalent) - i.e. only when the *first* agent spawn of an attempt fails with capacity language, before any auto-fix commit side effects from that attempt could have landed. Explicitly out of scope (`Must NOT Have`): rolling back or reasoning about partial auto-fix commits from a failed attempt; the next candidate simply re-runs against current worktree state, mirroring what a manual retry would do today. Add a QA scenario proving `review.Run` tolerates being invoked twice in sequence against the same worktree.
- **D8 (resolves missing constraint: probe containment posture)**: Presence/auth/quota probes run **uncontained** (no bwrap) - they are cheap, read-only status checks against the trusted host CLI's own auth state, never touching candidate/pushed worktree content, so the containment threat model (untrusted pushed content reaching agent CLI filesystem access) doesn't apply. Each probe gets a short fixed timeout (5s); timeout is treated as a probe failure (skip candidate), not a hard error. Recorded as a code comment explaining why this differs from the real review spawn's containment.
- **D9 (resolves missing constraint: quota-axi external dependency / testability)**: `quota-axi` stays optional and PATH-probed only (`exec.LookPath`), never added to `mise.toml`/Dockerfile/CI. All quota-parsing tests use **golden JSON fixtures** (the real captured claude-authenticated and codex-unauthenticated responses above, saved under `internal/agent/testdata/quotaaxi/`), never the live binary in CI; a `MADE_AGENT_LIVE_TEST`-style guard covers an optional live smoke test on a host that has it. `schemaVersion: 5` is documented in a comment as the pinned shape this parser understands; an unrecognized future major version is treated as "no quota signal" (fail open to "don't block on it"), never a hard error.
- **D10 (resolves ambiguity: quota-exhausted threshold)**: Single threshold, `< 1.0` percent remaining, used identically for both the primary `effectiveAvailability[scope=="all_models"].effectivePercentRemaining` path and the raw-`windows[]` fallback path (used only if `quotaSemantics` is absent entirely or `effectiveAvailability` is empty-but-status-unknown, e.g. old quota-axi schema). `status != "known"` (e.g. codex unauthenticated) means "no quota signal available" - never treated as exhausted (the auth-probe step already caught that case for codex/claude; for cursor/grok this step never runs auth first, so an unauthenticated cursor/grok simply won't have usable quota data either - also skip-quota-check-gracefully, consistent with D2).
- **D11 (resolves ambiguity: candidate-order precedence when both agent: and agents: set)**: Already fully specified by the brief, restated precisely: `Agent` a real, non-`auto`/non-empty kind -> that kind only, `Agents` ignored entirely, zero probing (today's exact behavior). Otherwise (`Agent` empty or `"auto"`) -> `Agents` if non-empty, else `SupportedKinds()`'s fixed default order.
- **D12 (resolves ambiguity: BinaryPath/ExtraEnv single-valued semantics)**: `review.Options.BinaryPath`/`managed`'s `req.AgentBinaryPath`/`AgentExtraEnv` remain exactly as they are today (single-valued, existing test-injection seam) - when set, they pin resolution to exactly one kind with **zero probing**, treated identically to an explicit `agent:` pin. This preserves every existing test using this seam untouched; the new probing/fallback path only engages when no such override is present.
- **D13 (resolves execution risk: fake-agent harness structural limitation)**: New multi-binary test harness is Task 2 (Wave 1), a hard prerequisite for every resolver/fallback test.
- **D14 (resolves missing constraint: evidence collision across attempts)**: Namespace per-attempt evidence filenames by kind (e.g. `review-response.<kind>.json` for a failed attempt) in both `internal/pipeline/review/evidence.go` and managed's `WriteStageFiles` call site; the final successful attempt's evidence is additionally written at today's canonical unsuffixed name for backward compatibility with existing consumers.
- **D15 (resolves missing constraint: timeout budget across candidates)**: No new per-candidate timeout knob. The existing `Config.StageTimeout("review")` context deadline already bounds the whole resolution+attempt loop; if it expires mid-loop, the loop aborts with the existing deadline-exceeded behavior (same as any long stage today). Recorded in `Must NOT Have`.
- **D16 (resolves missing constraint: kill-switch)**: No new config knob needed - pinning `agent: <kind>` already fully disables resolution/probing (D11), which **is** the rollback path. Documented, not built.
- **D17 (resolves missing constraint: probe caching/concurrency)**: No caching/memoization - every resolution call re-probes fresh, matching quota's inherently point-in-time nature (brief's own text already acknowledges quota can change between probe and real run). Accepted, not a defect.
- **D18 (resolves missing constraint: success-path attribution)**: The new `AgentResolution` struct (Task 10) is used for **both** outcomes, not just failure: on success it records `{selected: kind, attempts: [...]}`; on all-exhausted failure, `{selected: null, attempts: [...]}`. One shape, wired into `internal/managed/reviewsource.go:102`'s hardcoded `Executor: "made"` (extended to also carry the resolved kind) and into the daemon pipeline's per-run summary, so a successful fallback is auditable.
- **D19 (resolves topology gap: made verify / capabilities / cursor doctor scope)**: `made verify run` inherits resolution for free (it already calls `managed.BuildStagePlan`/`Runner` directly per this repo's own CLAUDE.md, so updating `managed/runner.go:208`'s call site covers it) - no separate verify-specific flag. `verify prepare`/`complete` (external-review-only) are explicitly out of scope. `made capabilities --json` schema changes and `made cursor doctor` agent-availability checks are explicitly **out of scope** (`Must NOT Have`) - follow-up work, not requested by the brief's 5 numbered build items.
- **D20 (resolves topology gap: human-readable output)**: Add one clear line to whatever existing human-mode stage-failure/summary output already exists (daemon pipeline's human path, `made verify`'s human path) summarizing either the resolved kind or the per-candidate reasons on exhaustion - reusing existing output plumbing, not a new subsystem.
- **D21 (resolves topology gap: documentation)**: Add a short section to this repo's `AGENTS.md` (per this task's own "Project memory" instructions) covering `agent: auto` + `agents: [...]` semantics and the push-option preference, pointing at `internal/agent/resolve.go` as the authoritative source - no new `docs/*.md` file needed for this repo (project convention observed: `AGENTS.md` already carries this kind of dense architecture note).
- **D22 (resolves execution risk: dogfooding blast radius)**: This repo's own `.made.yaml` (`agent: codex`, `review.required: true`) is **not modified** by this task (`Must NOT Have`) - avoids `internal/cursor/committed_test.go`'s generator-drift CI check entirely, since that only fires when `.made.yaml`'s cursor/guide config or the generator inputs change. Delivery is direct-PR per this task's own instructions (no `made` pipeline run required on this diff); local `go build`/`go vet`/`go test ./...`/`golangci-lint run` are still run before pushing as ordinary hygiene.

### Test Strategy
RED then GREEN per behavioral claim, this repo's existing pattern: plain `testing` package (no testify), table/fixture-const style, `t.TempDir()`, black-box `_test` packages where the existing file already uses one. New multi-binary fake-CLI harness (Task 2) is itself TDD'd first since every later resolver test depends on it.

## Work Objectives

### Core Objective
`made` autodetects a working review-agent CLI from an ordered candidate list, skips ones that are missing/unauthenticated/quota-exhausted, falls back mid-run on a classified capacity error, and reports a structured (never generic) reason when every candidate is exhausted - all while preserving today's exact behavior, byte-for-byte, when a single non-`auto` `agent:` is pinned.

### Deliverables
- `internal/config/config.go`: `agent: auto` sentinel semantics, `Agents` real candidate-list consumption, `validateReviewRequiresAgent` relaxed (D1).
- `internal/agent/agenttest`: new multi-binary (`codex`/`claude`/`cursor-agent`/`grok`) fake-CLI test harness supporting presence, `login status`/`auth status` subcommand behavior, and existing review-invocation behavior.
- `internal/agent/quotaaxi.go` (or similar): optional `quota-axi` probe + parser, golden-fixture-tested.
- `internal/agent/resolve.go`: per-candidate resolution (presence -> auth -> quota, D2/D10/D11), producing the shared `AgentResolution` result (D18).
- `internal/agent/spawn.go`: `ErrAgentCapacity` classification (codex+claude stderr patterns, D2) - no change to any other failure's classification.
- `internal/orchestrator/workfunc.go` + `internal/managed/runner.go`: both `AgentKind()` call sites replaced with pin-fast-path-or-resolve (D11/D12), mid-run fallback retry loop scoped to primary spawn only (D7).
- Push-option layer: `internal/gitgate/bare.go` (D5 self-heal), `internal/gitgate/hook.go` (parse+forward), `cmd/made/gate.go` (new flag), `daemon.go`'s `gateNotifyPushParams`, `orchestrator.Options.AgentPreference` (new field, `CandidateOutputSHA` precedent), trust-boundary gating (D4).
- Structured surfacing: `AgentResolution`/`CandidateAttempt` types, additive fields on `internal/daemon/runstate.go`'s `StageResult`, `internal/managed/contract.go`'s `StageResult` (+ `OutcomeInfrastructureError` reuse, D3), `internal/verify/receipt.go`'s `BuildReceipt` copy-through.
- Evidence namespacing per attempt (D14).
- `made doctor` (both human + JSON paths, `cmd/made/doctor.go`) reports resolved/attempted agent.
- Human-readable summary line (D20).
- `AGENTS.md` documentation update (D21).
- Full RED/GREEN test suite per behavioral claim.

### Definition of Done (verifiable conditions with commands)
- `go build ./...` succeeds.
- `go vet ./...` clean.
- `golangci-lint run` clean (repo's configured linters).
- `GIT_CONFIG_COUNT=1 GIT_CONFIG_KEY_0=commit.gpgsign GIT_CONFIG_VALUE_0=false go test ./...` all green, including every new test listed below.
- `git diff --stat -- .made.yaml` empty (D22 - config untouched).
- Manual smoke: a config with `agent: auto` + `agents: [claude, codex]` where `codex` binary is absent from PATH resolves to `claude` (verified via a resolver unit test, not a live run).

### Must Have
- Explicit non-`auto` `agent:` pin: zero probing overhead, zero behavior change from today (existing tests for this stay green untouched).
- `agent: auto`/empty + `agents: [...]`: probe in the given order.
- `agent: auto`/empty + empty `agents:`: probe `SupportedKinds()`'s fixed default order.
- Push-option preference honored only when `AllowRepoCommands: true` (D4).
- `ErrAgentCapacity` fallback only ever masks a *capacity* failure, never any other failure class - a non-capacity error from the last-standing candidate is still a hard failure exactly as today.
- Structured all-candidates-exhausted reason is JSON-visible on all 3 surfaces (daemon `StageResult`, managed `StageResult`/`Outcome`, verify `StageReceipt`) via additive fields only.

### Must NOT Have
- No new `docs/*.md` file (D21 uses `AGENTS.md`).
- No new config knob for kill-switch, per-candidate timeout budget, or probe caching (D15/D16/D17).
- No rollback/undo of a failed attempt's partial auto-fix commits (D7).
- No auth-status invention for `cursor`/`grok` beyond `LookPath` (D2).
- No new `managed.Outcome` enum value (D3).
- No changes to `made capabilities --json`, `made cursor doctor`, `verify prepare`/`complete`, or this repo's own `.made.yaml` (D19/D22).
- No live `quota-axi`/CLI-auth network calls inside the default `go test ./...` run (fixtures only, D9).

## Verification Strategy
> ZERO HUMAN INTERVENTION - all verification is agent-executed.
- Test decision: tests-after per implementation task (existing pattern in this repo is implementation+test as one commit), RED then GREEN shown per behavioral claim in each task's QA scenarios.
- Framework: plain `testing`, `t.TempDir()`, table/fixture-const style matching `internal/config/config_test.go` / `internal/agent/agent_test.go` exactly.
- Evidence: `evidence/task-{N}-{slug}.txt` (command transcripts / test output), per task.

## Execution Strategy
### Parallel Execution Waves
Wave 1 (foundation, fully parallel): Tasks 1-5
Wave 2 (resolver + call-site wiring, depends on Wave 1): Tasks 6-9
Wave 3 (push-option layer + structured surfacing + doctor + docs, depends on Wave 2): Tasks 10-16
Wave 4 (remaining tests not already embedded in earlier tasks + final verification): Tasks 17-19, F1-F4

### Dependency Matrix
- Task 1 (config schema) -> blocks Task 6, 7, 8
- Task 2 (multi-binary harness) -> blocks Task 6, 7's tests, 8's tests, 9's tests
- Task 3 (quota-axi parser+fixtures) -> blocks Task 6
- Task 4 (ErrAgentCapacity classification) -> blocks Task 7, 8
- Task 5 (shared AgentResolution/CandidateAttempt types) -> blocks Task 6, 10
- Task 6 (resolve.go) -> blocks Task 7, 8, 12
- Task 7 (workfunc.go wiring) -> blocks Task 10 (daemon surfacing), 17
- Task 8 (managed/runner.go wiring) -> blocks Task 10 (managed/verify surfacing), 17
- Task 9 (evidence namespacing) -> depends on Task 7/8, blocks nothing further
- Task 10 (structured surfacing: daemon+managed+verify) -> depends on 5,7,8, blocks 18
- Task 11 (push-option: bare.go + hook.go) -> independent of 6-10, blocks Task 12
- Task 12 (push-option: gate.go flag + daemon.go params + orchestrator.Options + trust gating) -> depends on 6 (resolve), 11
- Task 13 (made doctor) -> depends on 6
- Task 14 (human-readable summary line) -> depends on 10
- Task 15 (AGENTS.md docs) -> depends on all prior (describes final shape)
- Task 16 (config validator + precedence tests) -> can run alongside Task 1 as its test half
- Task 17 (mid-run fallback + all-exhausted tests) -> depends on 6,7,8
- Task 18 (push-option + offline-queue-drop tests) -> depends on 11,12
- Task 19 (doctor + evidence-namespacing tests) -> depends on 9,13
- F1-F4 depend on everything

## TODOs

- [x] 1. Config schema: `agent: auto` sentinel + real `agents:` semantics + validator fix (D1, D11)

  **What to do**: In `internal/config/config.go`:
  - Add `func (c Config) AgentIsPinned() bool { return c.Agent != "" && c.Agent != "auto" }`.
  - Add `func (c Config) AgentCandidates() []agent.Kind` implementing D11 exactly: if `!AgentIsPinned()`, return `agent.ParseKind` of each entry in `c.Agents` (already validated in `validateCommon`, safe to ignore errors here) if `len(c.Agents) > 0`, else `agent.SupportedKinds()`. Do not touch `AgentKind()` - it stays exactly as-is for the pinned fast path.
  - Relax `validateReviewRequiresAgent` (config.go:218-223) per D1: remove the hard error for `Agent == ""`/`"auto"` entirely - auto-detect is always a valid selection mechanism. Keep `validateCommon`'s existing `ParseKind` check for an explicit-but-invalid `Agent` value (unchanged, config.go:226-230).
  - Confirm `"auto"` is treated as a synonym for `""` everywhere `Agent` is read for pin-detection (only `AgentIsPinned`/`AgentCandidates` need to know about the literal string `"auto"` - `AgentKind()` and `validateCommon`'s `ParseKind` call are only ever invoked on the pinned path, so they never see `"auto"`).

  **Must NOT do**: Do not change `AgentKind()`'s signature or fail-closed contract. Do not add a new YAML key. Do not make `"auto"` a member of `agent.Kind`'s enum (it is a config-layer sentinel only, never reaches `internal/agent`).

  **Parallelization**: Can Parallel: YES | Wave 1 | Blocks: 6, 7, 8, 16 | Blocked By: none

  **References**:
  - `internal/config/config.go:37-38,194-243` - existing `Agent`/`Agents` fields, `AgentKind()`, `Validate()`, `validateReviewRequiresAgent()`, `validateCommon()`.
  - `internal/agent/agent.go` - `Kind`, `ParseKind`, `SupportedKinds()`, `SupportedKindNames()`.
  - `internal/config/config_extended_test.go:183-197` - existing `AgentKind()` fail-closed test, must stay green unmodified.

  **Acceptance Criteria**:
  - [ ] `go build ./internal/config/...` succeeds.
  - [ ] `config_extended_test.go`'s existing `AgentKind()` tests pass unmodified.
  - [ ] A config with `review.required: true`, `agent: ""`, `agents: [claude, codex]` passes `Config.Validate()`.
  - [ ] A config with `review.required: true`, `agent: ""`, `agents: []` passes `Config.Validate()` (falls back to `SupportedKinds()`).
  - [ ] A config with `agent: bogus` still fails `Config.Validate()` (unchanged `ParseKind` check).

  **QA Scenarios**:
  ```
  Scenario: agent:auto + agents:[claude,codex], review.required:true
    Tool: go test
    Steps: table-test in config_test.go loading that exact YAML via t.TempDir()+writeConfigFile, call Config.Validate()
    Expected: nil error
    Evidence: evidence/task-1-config-validate.txt

  Scenario: agent:bogus still rejected
    Tool: go test
    Steps: table-test loading agent: bogus, call Config.Validate()
    Expected: non-nil error mentioning bogus/supported kinds
    Evidence: evidence/task-1-config-validate-reject.txt
  ```

  **Commit**: YES | Message: `feat(config): add agent:auto sentinel and real agents: candidate list` | Files: `internal/config/config.go`, `internal/config/config_test.go`

- [x] 2. New multi-binary fake-CLI test harness (D13)

  **What to do**: Extend `internal/agent/agenttest` (new file, e.g. `agenttest/multiharness.go`) to build a *set* of differently-named fake binaries (`codex`, `claude`, `cursor-agent`, `grok` - match each `Kind`'s real binary name from `internal/agent/agent.go`) into a temp directory added to `PATH` for a test. Each binary must, based on `argv[0]` (or a baked-in build tag/const per binary, whichever is simpler to implement with `go build -o <dir>/<name>` from one shared `main.go` source parameterized by an env var set differently per test, or by building N copies with different `-ldflags -X` values), support:
  - No args / review-invocation args: reuse existing `fakeagent` envelope/scenario logic (delegate to the existing `main.go` machinery, do not duplicate it).
  - `login status` (codex) / `auth status` (claude) subcommand: exit 0 or nonzero based on a new env var (e.g. `FAKE_AGENT_AUTH_STATUS=0|1`) read per-binary-name at process start.
  - A helper `agenttest.BuildFleet(t *testing.T, present map[agent.Kind]FleetOptions) (pathDir string)` where `FleetOptions{AuthExitCode int, Scenario ...}` and an absent kind simply has no binary written for it (so `exec.LookPath` fails naturally on the test's scoped `PATH`).

  **Must NOT do**: Do not modify the existing single-binary `agenttest.Build(t)` helper or any test that already uses it - this is additive. Do not make the new harness a hard dependency of any existing test.

  **Parallelization**: Can Parallel: YES | Wave 1 | Blocks: 6 (tests), 7 (tests), 8 (tests), 17, 18 | Blocked By: none

  **References**:
  - `internal/agent/agenttest/agenttest.go:17-36` - existing single-binary builder to extend/model after.
  - `internal/agent/testdata/fakeagent/main.go` - existing envelope/scenario logic to reuse via a shared build, not duplicate.
  - `internal/agent/agent.go` - real per-`Kind` binary names (`codex`, `claude`, `cursor-agent`, `grok`) - confirm exact binary name strings here, do not guess.

  **Acceptance Criteria**:
  - [ ] `BuildFleet` compiles and places exactly the requested subset of binary names on a scoped `PATH`.
  - [ ] A binary invoked as `login status`/`auth status` exits with the configured code and no other side effects.
  - [ ] A binary invoked with review-invocation args still produces the same structured findings envelope as today's `fakeagent`.
  - [ ] `exec.LookPath` against the scoped `PATH` fails for a kind not included in the fleet.

  **QA Scenarios**:
  ```
  Scenario: fleet with claude present+authed, codex absent
    Tool: go test
    Steps: BuildFleet(t, map[Kind]FleetOptions{KindClaude: {AuthExitCode: 0}}), then exec.LookPath("codex") and exec.LookPath("claude") against that PATH
    Expected: codex lookup errors (not found), claude lookup succeeds
    Evidence: evidence/task-2-fleet-presence.txt

  Scenario: claude present but unauthed
    Tool: go test
    Steps: BuildFleet with FleetOptions{AuthExitCode: 1}, run "<path>/claude auth status"
    Expected: exit code 1, no panic/crash
    Evidence: evidence/task-2-fleet-auth-status.txt
  ```

  **Commit**: YES | Message: `test(agent): add multi-binary fake-CLI fleet harness for resolver tests` | Files: `internal/agent/agenttest/multiharness.go`, `internal/agent/agenttest/multiharness_test.go`, `internal/agent/testdata/fakeagent/main.go` (if shared logic needs minor extraction)

- [x] 3. `quota-axi` optional probe + parser + golden fixtures (D9, D10)

  **What to do**: New `internal/agent/quotaaxi.go`. `func probeQuota(ctx context.Context, kind Kind) (*QuotaSignal, error)`: `exec.LookPath("quota-axi")` first - if absent, return `(nil, nil)` (no signal, never an error). If present, run `quota-axi --provider <kind-string> --json --full --no-credential-refresh` with a 5s timeout (D8), parse JSON into a minimal struct covering only the fields needed: `providers[].quotaSemantics.{status, effectiveAvailability[].{scope, status, effectivePercentRemaining}}` and `providers[].windows[].percentRemaining` (fallback path). Implement exactly per D9/D10: prefer `effectiveAvailability` entry with `scope == "all_models"` when its `status == "known"`; else fall back to scanning raw `windows[]` for any `percentRemaining < 1`; else (nothing usable) return `(nil, nil)` - no signal, never blocks a candidate. Add a code comment recording the D9/D10 reasoning (why `all_models` + why this fallback order) since this is the documented judgment call the brief calls out.

  **Must NOT do**: Do not call `quota-axi` without `--no-credential-refresh`. Do not treat quota-axi's absence, a non-zero exit, unparseable JSON, or `status != "known"` as an error that fails the candidate - all of these mean "no quota signal," not "quota exhausted." Do not add `quota-axi` to `mise.toml`/Dockerfile/CI.

  **Parallelization**: Can Parallel: YES | Wave 1 | Blocks: 6 | Blocked By: none

  **References**:
  - Real captured fixtures (save verbatim as testdata, from this planning session's live `quota-axi --provider claude --json --full --no-credential-refresh` and `--provider codex` output): claude-authenticated example has one `effectiveAvailability` entry `scope: "all_models"`, `status: "known"`, `effectivePercentRemaining: 28`; codex-unauthenticated example has `windows: []`, `quotaSemantics: {status: "unknown", effectiveAvailability: []}`.
  - `internal/agent/agent.go` - `Kind` string values used as `--provider` argument (confirm the exact provider name string per kind matches quota-axi's accepted values `claude,codex,cursor,copilot,grok,kimi,zai,agy,alibaba,opencode-go` - `cursor` and `grok` map directly, no translation needed).

  **Acceptance Criteria**:
  - [ ] Parsing the saved claude-authenticated fixture yields `effectivePercentRemaining: 28`, not exhausted (>= 1).
  - [ ] Parsing the saved codex-unauthenticated fixture yields `(nil, nil)` - no signal.
  - [ ] A fixture with `effectivePercentRemaining: 0.5` on the `all_models` scope yields "exhausted."
  - [ ] `quota-axi` binary absent from `PATH` (simulated via scoped `PATH` in test) yields `(nil, nil)`, no error.

  **QA Scenarios**:
  ```
  Scenario: quota-axi absent from PATH
    Tool: go test
    Steps: run probeQuota with a scoped empty PATH
    Expected: (nil, nil) returned, no error, no panic
    Evidence: evidence/task-3-quota-absent.txt

  Scenario: golden fixture parsing (both fixtures)
    Tool: go test
    Steps: feed both saved JSON fixtures through the parser directly (no subprocess)
    Expected: claude fixture -> effectivePercentRemaining=28, not exhausted; codex fixture -> nil signal
    Evidence: evidence/task-3-quota-fixtures.txt
  ```

  **Commit**: YES | Message: `feat(agent): add optional quota-axi probe with golden-fixture parser` | Files: `internal/agent/quotaaxi.go`, `internal/agent/quotaaxi_test.go`, `internal/agent/testdata/quotaaxi/claude_authenticated.json`, `internal/agent/testdata/quotaaxi/codex_unauthenticated.json`

- [x] 4. `ErrAgentCapacity` classification in `spawn.go` (D2)

  **What to do**: In `internal/agent/spawn.go`, define `var ErrAgentCapacity = errors.New("agent: capacity exhausted")` (or a small typed error wrapping it, e.g. `type CapacityError struct { Kind Kind; Detail string }` implementing `Unwrap() error { return ErrAgentCapacity }` so `errors.Is(err, ErrAgentCapacity)` works). At the existing non-zero-exit failure site (~line 92-93), after building today's generic wrapped error, additionally check the captured stderr against a small **per-kind, verified-only** pattern table restricted to `codex` and `claude` (D2) - e.g. case-insensitive substring match for language like "usage limit", "rate limit", "quota" for claude; "usage limit reached", "rate limit" for codex (record in a code comment that these are best-effort patterns based on the brief's own examples, not independently verified against a real exhaustion event, and that `cursor`/`grok` are deliberately excluded from this classification, D2). When matched, wrap the returned error so `errors.Is(returnedErr, ErrAgentCapacity)` is true, while preserving the existing generic error message/formatting for anything that doesn't match (no change to any other failure's shape or text).

  **Must NOT do**: Do not classify any failure other than the specific stderr-pattern match as capacity - a missing binary, a non-matching nonzero exit, or a context-deadline error must remain exactly today's generic hard failure. Do not add patterns for `cursor`/`grok`.

  **Parallelization**: Can Parallel: YES | Wave 1 | Blocks: 7, 8, 17 | Blocked By: none

  **References**:
  - `internal/agent/spawn.go:81-95` (approx, per verified research) - existing generic failure wrapping to extend, not replace.
  - `internal/agent/agenttest` (Task 2) - the fleet harness's fake binaries need a way to emit a specific stderr string + nonzero exit to drive this classification in tests (add a `FleetOptions.Stderr string` / reuse existing `FAKE_AGENT_*` env plumbing).

  **Acceptance Criteria**:
  - [ ] A nonzero exit with stderr containing "usage limit" (claude) or "usage limit reached" (codex) returns an error satisfying `errors.Is(err, ErrAgentCapacity)`.
  - [ ] A nonzero exit with unrelated stderr (e.g. "invalid argument") returns today's generic error, `errors.Is(err, ErrAgentCapacity)` is false.
  - [ ] A `cursor`/`grok` nonzero exit is never classified as capacity regardless of stderr content (documented limitation, D2).

  **QA Scenarios**:
  ```
  Scenario: claude quota-language stderr classified as capacity
    Tool: go test
    Steps: fake claude binary exits 1 with stderr "Claude usage limit reached, try again later"
    Expected: SpawnWithEvidence returns error where errors.Is(err, ErrAgentCapacity) is true
    Evidence: evidence/task-4-capacity-classified.txt

  Scenario: unrelated claude failure not classified as capacity
    Tool: go test
    Steps: fake claude binary exits 1 with stderr "invalid schema"
    Expected: errors.Is(err, ErrAgentCapacity) is false, error text unchanged from today's format
    Evidence: evidence/task-4-capacity-not-classified.txt
  ```

  **Commit**: YES | Message: `feat(agent): classify quota/rate-limit stderr as ErrAgentCapacity for codex+claude` | Files: `internal/agent/spawn.go`, `internal/agent/spawn_test.go`

- [x] 5. Shared `AgentResolution`/`CandidateAttempt` types (D18)

  **What to do**: New `internal/agent/resolution.go` defining the one shared shape used for both success and failure surfacing (D18):
  ```go
  type AttemptReason string
  const (
      ReasonMissing         AttemptReason = "missing"
      ReasonUnauthenticated AttemptReason = "unauthenticated"
      ReasonQuotaExhausted  AttemptReason = "quota_exhausted"
  )
  type CandidateAttempt struct {
      Kind         Kind          `json:"kind"`
      Reason       AttemptReason `json:"reason,omitempty"`
      QuotaResetsAt *time.Time   `json:"quota_resets_at,omitempty"`
  }
  type AgentResolution struct {
      Selected *Kind              `json:"selected,omitempty"`
      Attempts []CandidateAttempt `json:"attempts"`
  }
  func (r AgentResolution) AllExhausted() bool { return r.Selected == nil }
  ```
  This is a pure data type with no I/O - the resolver (Task 6) constructs it, callers (Tasks 7, 8, 10, 13) consume it.

  **Must NOT do**: Do not give this type any behavior beyond `AllExhausted()` - no formatting/marshaling helpers beyond what `encoding/json` gives for free (keep it a plain struct so it's trivially embeddable in the additive fields added in Task 10).

  **Parallelization**: Can Parallel: YES | Wave 1 | Blocks: 6, 10 | Blocked By: none

  **References**: None beyond the brief's own escalation requirement (item 4: "naming every candidate tried and why each failed: missing/unauthenticated/quota-exhausted-until-<resetsAt>").

  **Acceptance Criteria**:
  - [ ] `json.Marshal` of an `AgentResolution{Selected: &KindClaude, Attempts: [...]}` produces the expected shape (spot-check field names).
  - [ ] `json.Marshal` of an all-exhausted `AgentResolution{Selected: nil, Attempts: [...]}` omits `selected` entirely (`omitempty` on a nil pointer).

  **QA Scenarios**:
  ```
  Scenario: success shape marshals with selected kind
    Tool: go test
    Steps: marshal AgentResolution{Selected: &KindClaude, Attempts: [{Kind: KindCodex, Reason: ReasonMissing}]}
    Expected: JSON contains "selected":"claude" and one attempt with "reason":"missing"
    Evidence: evidence/task-5-resolution-json.txt

  Scenario: all-exhausted shape omits selected
    Tool: go test
    Steps: marshal AgentResolution{Selected: nil, Attempts: [...]}
    Expected: JSON has no "selected" key
    Evidence: evidence/task-5-resolution-json-exhausted.txt
  ```

  **Commit**: YES | Message: `feat(agent): add shared AgentResolution/CandidateAttempt types` | Files: `internal/agent/resolution.go`, `internal/agent/resolution_test.go`

- [x] 6. `internal/agent/resolve.go`: per-candidate resolver (D2, D8, D10, D11, D12)

  **What to do**: `func Resolve(ctx context.Context, candidates []Kind, opts ResolveOptions) AgentResolution` where `ResolveOptions` carries whatever's needed to route probes through the fleet harness in tests (e.g. an optional `PATH` override, matching how `BinaryPath`/`ExtraEnv` already work per D12 - **this function is only ever called on the non-pinned path**; the pinned fast path (D11/D12) never calls it). For each candidate in order:
  1. Presence: `exec.LookPath` (respecting `ResolveOptions`' PATH override if set) - miss -> `CandidateAttempt{Kind, Reason: ReasonMissing}`, continue.
  2. Auth (codex/claude only, D2): run `codex login status` / `claude auth status` with a 5s timeout, uncontained (D8) - nonzero exit or timeout -> `ReasonUnauthenticated`, continue. `cursor`/`grok`: skip this step entirely (presence-only, D2).
  3. Quota (D10): call `probeQuota` (Task 3) - a signal indicating exhausted -> `CandidateAttempt{Kind, Reason: ReasonQuotaExhausted, QuotaResetsAt: ...}`, continue; `(nil, nil)` (no signal) or not-exhausted -> proceed.
  4. First candidate to pass all applicable steps -> `AgentResolution{Selected: &kind, Attempts: attemptsSoFar}` (only attempts for *skipped* candidates are recorded, per brief's "naming every candidate tried and why each failed" - the selected one isn't a failure attempt).
  If no candidate passes -> `AgentResolution{Selected: nil, Attempts: allAttempts}`.

  **Must NOT do**: Do not probe quota for a candidate that already failed presence/auth (short-circuit, matches brief's lettered a/b/c ordering exactly - this also resolves Metis's "auth signals disagree" ambiguity: quota is never checked after an auth failure, so there's nothing to disagree about). Do not run probes inside the bwrap containment profile (D8). Do not memoize/cache across calls (D17).

  **Parallelization**: Can Parallel: NO (depends on 1, 2, 3, 4, 5 all landing first) | Wave 2 | Blocks: 7, 8, 12, 13 | Blocked By: 1, 2, 3, 4, 5

  **References**:
  - `internal/agent/agent.go` - `Kind`, per-kind binary names.
  - Task 2's fleet harness - the only way to drive multi-candidate scenarios in tests.
  - Task 3's `probeQuota`, Task 4's `ErrAgentCapacity` (used only by the *runtime fallback*, Task 7/8, not by this pre-probe resolver - the resolver's quota check is proactive/pre-run, the `ErrAgentCapacity` classification is reactive/mid-run; both exist because quota can't always be pre-probed per the brief).

  **Acceptance Criteria**:
  - [ ] Candidate list `[codex, claude]`, codex absent, claude present+authed+quota-ok -> `Selected: claude`, one attempt `{codex, missing}`.
  - [ ] Candidate list `[claude, codex]`, claude present but `auth status` exits 1, codex present+authed -> `Selected: codex`, one attempt `{claude, unauthenticated}`.
  - [ ] Candidate list `[claude]`, claude present+authed, quota fixture shows exhausted -> `Selected: nil`, one attempt `{claude, quota_exhausted}`.
  - [ ] Candidate list `[cursor]`, cursor present -> `Selected: cursor` (no auth step attempted, D2).
  - [ ] Candidate list `[cursor]`, cursor absent -> `Selected: nil`, one attempt `{cursor, missing}`.

  **QA Scenarios**:
  ```
  Scenario: skip missing, select next
    Tool: go test
    Steps: BuildFleet(t, {claude: authed+quota-ok}), Resolve(ctx, [codex, claude], opts)
    Expected: AgentResolution{Selected: &claude, Attempts: [{codex, missing}]}
    Evidence: evidence/task-6-resolve-skip-missing.txt

  Scenario: skip unauthenticated, select next
    Tool: go test
    Steps: BuildFleet(t, {claude: unauthed, codex: authed}), Resolve(ctx, [claude, codex], opts)
    Expected: AgentResolution{Selected: &codex, Attempts: [{claude, unauthenticated}]}
    Evidence: evidence/task-6-resolve-skip-unauth.txt

  Scenario: skip quota-exhausted, all exhausted
    Tool: go test
    Steps: BuildFleet(t, {claude: authed}), inject quota fixture showing exhausted via ResolveOptions test hook, Resolve(ctx, [claude], opts)
    Expected: AgentResolution{Selected: nil, Attempts: [{claude, quota_exhausted, quota_resets_at: <ts>}]}
    Evidence: evidence/task-6-resolve-all-exhausted.txt

  Scenario: cursor presence-only, no auth probe attempted
    Tool: go test
    Steps: BuildFleet(t, {cursor: present, no auth-status binary behavior configured}), Resolve(ctx, [cursor], opts)
    Expected: Selected: &cursor (selection succeeds purely on presence, proving no auth probe was attempted/required)
    Evidence: evidence/task-6-resolve-cursor-presence-only.txt
  ```

  **Commit**: YES | Message: `feat(agent): add Resolve() ordered-candidate probing (presence/auth/quota)` | Files: `internal/agent/resolve.go`, `internal/agent/resolve_test.go`

- [x] 7. Wire resolver + mid-run fallback into `workfunc.go:272` (D7, D11, D12)

  **What to do**: In `internal/orchestrator/workfunc.go`'s `reviewStage()`, replace the bare `c.rc.Config.AgentKind()` call: if `c.rc.Config.AgentIsPinned()`, call `AgentKind()` exactly as today (fast path, zero probing, zero behavior change - existing tests for this path must stay green unmodified). Otherwise, call `agent.Resolve(ctx, c.rc.Config.AgentCandidates(), opts)` (Task 6); if `AllExhausted()`, return a hard failure carrying the `AgentResolution` (consumed by Task 10, do not invent your own shape here). Otherwise, attempt `review.Run` with the selected kind; if it returns an error where `errors.Is(err, agent.ErrAgentCapacity)` (Task 4), and per D7 this is the *primary spawn* of that attempt (i.e. no partial fix-commit has landed for this specific candidate attempt - if `review.Run` doesn't already expose this distinction, scope the retry to only re-attempt when the error surfaces before any commit side effect is observable, and add a code comment stating this is D7's scope boundary), retry with the next candidate in the resolved list (re-running `Resolve`'s remaining candidates, not the full list again). Exhausting the retry loop -> same all-exhausted structured failure as above, now also carrying which candidates were tried live (not just pre-probed).

  **Must NOT do**: Do not retry on any non-`ErrAgentCapacity` error - that remains today's immediate hard failure. Do not attempt to undo any commits from a failed attempt (D7). Do not change the pinned fast path's error shape/text.

  **Parallelization**: Can Parallel: NO (depends on 1, 6) | Wave 2 | Blocks: 10, 17 | Blocked By: 1, 6

  **References**:
  - `internal/orchestrator/workfunc.go:269-282` - `reviewStage()`, the exact call site.
  - `internal/pipeline/review/review.go:51-66` - `review.Run`'s single-spawn-per-call shape, confirm exactly where its own internal error surfaces to `reviewStage()` so the D7 scope boundary can be implemented precisely (spawn error vs. post-spawn processing error).

  **Acceptance Criteria**:
  - [ ] Pinned `agent: claude` config: `reviewStage()` behavior/error text identical to before this change (regression test against current behavior).
  - [ ] `agent: auto`, `agents: [claude, codex]`, claude's spawn fails with a capacity-classified stderr, codex succeeds -> run succeeds using codex, resolution evidence shows the claude attempt.
  - [ ] `agent: auto`, `agents: [claude]`, claude's spawn fails with a *non*-capacity error -> hard failure, no retry attempted, error shape unchanged from today's single-agent failure.
  - [ ] All candidates capacity-fail -> structured all-exhausted failure (Task 10's shape), not a generic error.

  **QA Scenarios**:
  ```
  Scenario: pinned agent unaffected
    Tool: go test
    Steps: run reviewStage() with agent: claude pinned, fake claude succeeds
    Expected: byte-identical outcome to pre-change behavior (snapshot/compare)
    Evidence: evidence/task-7-pinned-unaffected.txt

  Scenario: mid-run capacity fallback succeeds on next candidate
    Tool: go test
    Steps: fleet with claude (capacity-failing stderr) + codex (succeeds), agents:[claude,codex], run reviewStage()
    Expected: success using codex; failure recorded for claude with reason quota_exhausted (or unauthenticated, whichever the capacity path maps to) in the surfaced resolution
    Evidence: evidence/task-7-midrun-fallback.txt

  Scenario: non-capacity failure is not retried
    Tool: go test
    Steps: fleet with single candidate claude, non-capacity nonzero exit
    Expected: reviewStage() returns hard failure immediately, no second candidate attempted (none configured, and no retry loop entered)
    Evidence: evidence/task-7-noncapacity-hardfail.txt
  ```

  **Commit**: YES | Message: `feat(orchestrator): resolve+fallback agent selection in reviewStage` | Files: `internal/orchestrator/workfunc.go`, `internal/orchestrator/workfunc_test.go`

- [x] 8. Wire resolver + mid-run fallback into `managed/runner.go:208` (D7, D11, D12)

  **What to do**: Mirror Task 7's exact logic in `internal/managed/runner.go`'s `Runner.Run`, at the `ReviewSourceInternal` branch (~line 208) - same pin-fast-path/resolve/fallback structure, same `AgentResolution` type, same `ErrAgentCapacity` retry scoping (D7). This is deliberately the same code shape as Task 7 (consider extracting a small shared helper in `internal/agent` if the two call sites turn out identical enough - do not force a shared helper if `internal/orchestrator` and `internal/managed`'s surrounding control flow differ enough to make it awkward; a little duplication between two call sites is acceptable per this repo's own stated preference for simplicity over premature abstraction).

  **Must NOT do**: Do not change `req.AgentBinaryPath`/`AgentExtraEnv`'s existing single-valued test-injection semantics (D12 - these still pin/override exactly as today, bypassing resolution entirely when set). Do not touch `ReviewSourceExternal`'s path at all.

  **Parallelization**: Can Parallel: NO (depends on 1, 6) | Wave 2 | Blocks: 10, 17 | Blocked By: 1, 6

  **References**:
  - `internal/managed/runner.go:208` - exact call site (`cfg.AgentKind()` today).
  - `internal/managed/runner.go` - `req.AgentBinaryPath`/`AgentExtraEnv` fields and how they already override agent invocation for tests (D12 precedent).

  **Acceptance Criteria**:
  - [ ] Pinned-agent managed run behavior unchanged (regression test).
  - [ ] `made verify run` (which calls this same engine per D19) with `agent: auto` + a capacity-failing first candidate falls back to the next candidate and succeeds.
  - [ ] All candidates exhausted -> `Outcome: OutcomeInfrastructureError` (D3) with the structured `AgentResolution` attached (Task 10).

  **QA Scenarios**:
  ```
  Scenario: managed pinned-agent unaffected
    Tool: go test
    Steps: Runner.Run with agent: claude pinned, ReviewSourceInternal, fake claude succeeds
    Expected: identical outcome to pre-change behavior
    Evidence: evidence/task-8-managed-pinned-unaffected.txt

  Scenario: managed mid-run fallback
    Tool: go test
    Steps: agents:[claude,codex], claude capacity-fails, codex succeeds
    Expected: Runner.Run succeeds via codex
    Evidence: evidence/task-8-managed-midrun-fallback.txt

  Scenario: managed all-exhausted maps to OutcomeInfrastructureError
    Tool: go test
    Steps: all configured candidates fail (mix of missing/unauth/quota)
    Expected: Outcome == OutcomeInfrastructureError, StageResult carries AgentResolution with Selected: nil
    Evidence: evidence/task-8-managed-all-exhausted.txt
  ```

  **Commit**: YES | Message: `feat(managed): resolve+fallback agent selection in Runner.Run` | Files: `internal/managed/runner.go`, `internal/managed/runner_test.go`

- [ ] 9. Evidence namespacing per attempt (D14)

  **What to do**: In `internal/pipeline/review/evidence.go`, when a fallback retry occurs (Task 7), write the *failed* attempt's evidence (`review-contract.json`, `review-prompt.txt`, `review-response.json`) suffixed with the attempted kind (e.g. `review-response.claude.json`) instead of the canonical unsuffixed name; the final successful attempt still writes the canonical unsuffixed names exactly as today (D14 - backward compatible with existing consumers that read the canonical names). Apply the same pattern to managed's `WriteStageFiles(stageReview, ...)` call site in `internal/managed/runner.go`.

  **Must NOT do**: Do not change the canonical (successful-attempt) filenames or their contents' shape - only add the suffixed files for failed attempts. Do not namespace evidence for the single-candidate (pinned) path at all (there's only ever one attempt there, no collision possible, no behavior change).

  **Parallelization**: Can Parallel: NO (depends on 7, 8 landing the retry loop that creates multiple attempts) | Wave 2/3 boundary | Blocks: none | Blocked By: 7, 8

  **References**:
  - `internal/pipeline/review/evidence.go:24-28` - canonical fixed evidence filenames.
  - `internal/managed`'s `WriteStageFiles(stageReview, ...)` call site (verify exact location/signature before editing).

  **Acceptance Criteria**:
  - [ ] A run with one failed attempt (claude) then a successful attempt (codex) leaves both `review-response.claude.json` (failed) and `review-response.json` (canonical, codex's) on disk, no overwrite.
  - [ ] A pinned single-candidate run's evidence filenames are byte-identical to today's (no suffix ever appears).

  **QA Scenarios**:
  ```
  Scenario: fallback run preserves both attempts' evidence
    Tool: go test
    Steps: run reviewStage() with a claude-then-codex fallback scenario, inspect evidence directory
    Expected: both review-response.claude.json and review-response.json present, distinct content
    Evidence: evidence/task-9-evidence-namespacing.txt

  Scenario: pinned run evidence filenames unchanged
    Tool: go test
    Steps: run with agent: claude pinned, inspect evidence directory
    Expected: only review-response.json (no suffixed variant ever written)
    Evidence: evidence/task-9-evidence-pinned-unchanged.txt
  ```

  **Commit**: YES | Message: `fix(agent): namespace per-attempt review evidence to avoid fallback overwrite` | Files: `internal/pipeline/review/evidence.go`, `internal/managed/runner.go`, associated `_test.go` files

- [ ] 10. Structured surfacing: daemon `StageResult`, managed `StageResult`/`Outcome`, verify `StageReceipt` (D3, D18)

  **What to do**: Add `AgentResolution *agent.AgentResolution \`json:"agent_resolution,omitempty"\`` to:
  - `internal/daemon/runstate.go`'s `StageResult` struct, populated by Task 7's `reviewStage()` on both success (D18 - selected kind recorded) and all-exhausted failure.
  - `internal/managed/contract.go`'s `StageResult` struct, populated by Task 8's runner at the review stage, same both-outcomes behavior; when all-exhausted, set `Outcome: OutcomeInfrastructureError` (D3) - do not add any new `Outcome` value.
  - `internal/verify/receipt.go`'s `StageReceipt` struct (mirroring `managed.StageResult`), with `BuildReceipt` (receipt.go:53-57) copying the field across.
  Also extend `internal/managed/reviewsource.go:102`'s hardcoded `Executor: "made"` to additionally carry the resolved kind (e.g. `Executor: "made/" + string(selectedKind)` or a new adjacent field - pick whichever is less invasive to existing `Executor` string consumers; grep for existing consumers of that exact field before choosing, to avoid breaking a string-equality check elsewhere).

  **Must NOT do**: Do not bump `TerminalManifest.SchemaVersion` or `ReceiptSchemaVersion` (D3/existing convention - additive `omitempty` fields only, per issue #61's precedent). Do not add a new `Outcome` enum value.

  **Parallelization**: Can Parallel: NO (depends on 5, 7, 8) | Wave 3 | Blocks: 14, 18 | Blocked By: 5, 7, 8

  **References**:
  - `internal/daemon/runstate.go:8-14` `StageResult`.
  - `internal/managed/contract.go:145-156` `StageResult`, `Outcome` enum definition (confirm exact location before editing).
  - `internal/verify/receipt.go:35-57` `Receipt`/`StageReceipt`, `BuildReceipt` (receipt.go:53-57).
  - `internal/managed/reviewsource.go:102` - `Executor: "made"` hardcode, plus a grep for every existing consumer of `StageResult`/`Receipt`'s `Executor`-equivalent field before changing its string shape.

  **Acceptance Criteria**:
  - [ ] A successful auto-resolved run's `StageResult`/`Receipt` JSON contains `agent_resolution.selected` matching the kind that actually ran.
  - [ ] An all-exhausted run's JSON contains `agent_resolution.selected` absent (omitted) and a non-empty `attempts` array with one entry per tried candidate and its reason.
  - [ ] `TerminalManifest.SchemaVersion` and `ReceiptSchemaVersion` are unchanged (still 3).
  - [ ] Existing consumers of `Executor`/equivalent (if any found by the grep) still parse/compare correctly after the change.

  **QA Scenarios**:
  ```
  Scenario: successful fallback is auditable
    Tool: go test
    Steps: run managed.Runner.Run with claude-then-codex fallback, inspect resulting StageResult/Receipt JSON
    Expected: agent_resolution.selected == "codex", attempts contains claude's failure reason
    Evidence: evidence/task-10-surfacing-success.txt

  Scenario: all-exhausted surfaces structured reason, no schema bump
    Tool: go test + diff
    Steps: run with every candidate failing, inspect StageResult/Receipt/TerminalManifest JSON; diff SchemaVersion fields against pre-change values
    Expected: agent_resolution present with full attempts list; SchemaVersion/ReceiptSchemaVersion both still 3
    Evidence: evidence/task-10-surfacing-exhausted.txt
  ```

  **Commit**: YES | Message: `feat(managed,daemon,verify): surface structured AgentResolution in stage results` | Files: `internal/daemon/runstate.go`, `internal/managed/contract.go`, `internal/managed/reviewsource.go`, `internal/verify/receipt.go`, associated `_test.go` files

- [ ] 11. Push-option layer, part 1: gate-side plumbing (D5)

  **What to do**: In `internal/gitgate/bare.go`'s `InitBare`, add `git config receive.advertisePushOptions true` (idempotent - safe to set on every init). Per D5, also add this same idempotent `git config` call at whatever point the daemon opens/uses an *existing* gate repo for a push (identify the exact call site during implementation - likely near where `notify-push`/gate-admit already touches the bare repo - so pre-existing gates self-heal with no manual migration). In `internal/gitgate/hook.go`'s generated `postReceiveScript`, add shell logic to read `GIT_PUSH_OPTION_COUNT` and each `GIT_PUSH_OPTION_<n>` (git sets these when push options were negotiated), find one of the form `agent=<value>` (first match wins if duplicated - document this), and forward it as a new `--agent-preference <value>` argument to `made gate notify-push` only when found (omit the flag entirely otherwise, for backward compatibility with the RPC's optional field).

  **Must NOT do**: Do not fail the push if no push option is present (default, unaffected case). Do not pass the raw `GIT_PUSH_OPTION_<n>` value into the command line without shell-quoting it correctly (Metis flagged this - value is attacker/pusher-controlled, avoid injection via correct shell quoting, e.g. pass through a variable expansion, not string concatenation into the command). Do not act on more than one ref's push options if multiple refs are pushed in one invocation beyond documenting current single-preference behavior (apply the same parsed value to every ref in that push, since `made` gates are single-branch in practice - confirm this assumption against `hook.go`'s loop before implementing, adjust if wrong).

  **Parallelization**: Can Parallel: YES | Wave 3 | Blocks: 12 | Blocked By: none (independent of 1-10)

  **References**:
  - `internal/gitgate/bare.go:11-30` `InitBare`.
  - `internal/gitgate/hook.go:36-44` `postReceiveScript` (exact current shell content, per verified research).
  - `cmd/made/gate.go:67-77` `runGateNotifyPushCommand` flag parsing (add `--agent-preference` as a new optional `fs.String`).
  - Git's push-option mechanics: `GIT_PUSH_OPTION_COUNT` + `GIT_PUSH_OPTION_0..N-1`, each a raw string set by the pushing client, populated in `post-receive`'s hook environment only when `receive.advertisePushOptions` is set server-side.

  **Acceptance Criteria**:
  - [ ] `git push -o agent=claude` against a freshly-`InitBare`'d gate does not error with "the receiving end does not support push options."
  - [ ] The generated `post-receive` script correctly extracts `agent=claude` from `GIT_PUSH_OPTION_0` and forwards `--agent-preference claude` to `notify-push`.
  - [ ] A push with no `-o` flag produces no `--agent-preference` argument at all (today's exact invocation, unaffected).
  - [ ] A push option value containing shell-metacharacters (test with something like `agent=claude;rm -rf /`) is safely forwarded as a literal string argument, never executed as shell.

  **QA Scenarios**:
  ```
  Scenario: push option round-trips through the hook
    Tool: bash + git (matching internal/gitgate/hook_test.go's existing pattern)
    Steps: init a bare gate, push with -o agent=claude, capture the exact argv the (stubbed/logged) notify-push invocation received
    Expected: argv includes --agent-preference claude
    Evidence: evidence/task-11-push-option-roundtrip.txt

  Scenario: shell-metacharacter push-option value is not executed
    Tool: bash + git
    Steps: push with -o "agent=claude$(touch /tmp/pwned)" against the test gate
    Expected: no /tmp/pwned file created; value passed through literally (likely rejected by ParseKind downstream, but never shell-executed)
    Evidence: evidence/task-11-push-option-injection-safe.txt
  ```

  **Commit**: YES | Message: `feat(gitgate): advertise and forward git push -o agent=<kind> preference` | Files: `internal/gitgate/bare.go`, `internal/gitgate/hook.go`, `cmd/made/gate.go`, associated `_test.go` files

- [ ] 12. Push-option layer, part 2: RPC/orchestrator plumbing + trust-boundary gating (D4, D6)

  **What to do**: Add `AgentPreference string \`json:"agent_preference,omitempty"\`` to `gateNotifyPushParams` (daemon.go:372-381) and to `daemon.RunSubmission` (built at daemon.go ~494-497, alongside the existing `CandidateOutputSHA` pattern - **not** to `daemon.GateSubmission`/the spool struct, per D6, since that's identity/dedup only). Add the same field to `internal/orchestrator.Options` (workfunc.go:47-50), following the `CandidateOutputSHA` precedent exactly (its doc comment already anticipates this). In `reviewStage()` (Task 7's call site), before consulting config resolution at all: if `Options.AgentPreference != ""` **and** `c.rc.Config.AllowRepoCommands` is true (D4 - reuse the exact existing pushed-config trust gate), treat it as a single-kind pin (identical fast path to an explicit `agent:` pin, D11/D12 semantics, no probing) - the preference wins over config entirely. If `AllowRepoCommands` is false, ignore the preference (do not error - just fall through to normal config-based resolution) and record in the `AgentResolution`/evidence that a preference was present but not honored (for auditability, per Metis). Explicitly test D6: an offline-queued submission (which travels through `daemon.GateSubmission`, no `AgentPreference` field) replays with the preference silently absent, falling back to config resolution - assert this exact behavior, not just document it.

  **Must NOT do**: Do not add `AgentPreference` to `daemon.GateSubmission`/the spool schema (D6). Do not error when the preference is ignored due to `AllowRepoCommands: false` - silently fall back (matches how every other pushed-config-only value already behaves in this trust model).

  **Parallelization**: Can Parallel: NO (depends on 6, 11) | Wave 3 | Blocks: 18 | Blocked By: 6, 11

  **References**:
  - `internal/daemon/daemon.go:372-381` `gateNotifyPushParams`, `~494-497` `RunSubmission` construction, `~485-488` the `work` closure building `orchestrator.Options{CandidateOutputSHA: ...}` (exact precedent to copy).
  - `internal/orchestrator/workfunc.go:47-50` `Options` struct + its doc comment (already anticipates this field).
  - `internal/config/file.go:72-80` - the existing `AllowRepoCommands`-gated trusted/pushed merge, the trust rule being reused (D4).
  - `internal/daemon/spool.go:16-22` `GateSubmission`, `queueOfflineGateSubmission` in `cmd/made/gate.go` (~line 135 per verified research) - confirm this is truly the offline-replay path before asserting D6's test.

  **Acceptance Criteria**:
  - [ ] `AllowRepoCommands: true` + `-o agent=claude` -> resolution uses claude with zero probing, regardless of config's own `agent`/`agents` values.
  - [ ] `AllowRepoCommands: false` + `-o agent=claude` -> preference ignored, normal config-based resolution runs instead, no error raised.
  - [ ] Offline-queued submission with a preference present at enqueue time replays without it (falls back to config resolution) - explicit assertion, not just a doc comment.

  **QA Scenarios**:
  ```
  Scenario: preference honored when repo commands allowed
    Tool: go test
    Steps: Options{AgentPreference: "claude"}, Config{AllowRepoCommands: true, Agent: "auto"}, run reviewStage()
    Expected: claude selected, no candidate probing occurred (verify via call-count assertion on the fleet/LookPath)
    Evidence: evidence/task-12-preference-honored.txt

  Scenario: preference ignored when repo commands disallowed
    Tool: go test
    Steps: Options{AgentPreference: "claude"}, Config{AllowRepoCommands: false, Agent: "auto", Agents: ["codex"]}
    Expected: codex selected (config's own resolution), no error, evidence notes preference was present-but-ignored
    Evidence: evidence/task-12-preference-ignored.txt

  Scenario: offline replay drops preference
    Tool: go test
    Steps: enqueue an offline GateSubmission-path push carrying a preference, drain/replay it, run resolution
    Expected: resolution falls back to config, no trace of the original preference (proving D6, not just asserting it by inspection)
    Evidence: evidence/task-12-offline-drop.txt
  ```

  **Commit**: YES | Message: `feat(daemon,orchestrator): thread push-option agent preference through trust-gated resolution` | Files: `internal/daemon/daemon.go`, `internal/orchestrator/workfunc.go`, associated `_test.go` files

- [ ] 13. `made doctor` reports resolved/attempted agent (brief item 5)

  **What to do**: In `cmd/made/doctor.go`, add one check to **both** `runDoctorCommand` (human text) and `runDoctorJSON` (JSON, populating `checks[...]`) - they're separately implemented, per verified research, so both need the edit. The check: locate config via existing `config.Locate`/`LoadEffectiveConfig` machinery already used elsewhere in doctor, call `Config.AgentIsPinned()`/`AgentCandidates()` + `agent.Resolve` (Task 6) read-only (no actual review run), and report either "resolved agent: `<kind>`" or, on all-exhausted, the full structured attempts list (human: one line per candidate; JSON: the `AgentResolution` struct verbatim under a new `checks["agent_resolution"]`-equivalent key, matching whatever shape doctor's JSON already uses for multi-value checks - inspect existing checks for a precedent, e.g. how `ghClient.AuthStatus` is reported, before choosing the exact key/value shape).

  **Must NOT do**: Do not have `made doctor` actually spawn a real review invocation - resolution probing only (presence/auth/quota), same read-only nature as doctor's other checks. Do not skip either of the two doctor implementations (D-nothing here, just don't forget the duplication Metis/research flagged).

  **Parallelization**: Can Parallel: NO (depends on 6) | Wave 3 | Blocks: 19 | Blocked By: 6

  **References**:
  - `cmd/made/doctor.go` - `runDoctorCommand` (human) and `runDoctorJSON` (JSON), `doctorReport{SchemaVersion, ProtocolVersion, Healthy, Checks}`, per verified research (154 lines total, both implementations duplicated).

  **Acceptance Criteria**:
  - [ ] `made doctor` (human mode) prints a line naming the resolved agent when resolution succeeds.
  - [ ] `made doctor --json` includes a machine-readable resolution result (selected kind or full attempts list) under a clearly-named key.
  - [ ] Doctor's overall `Healthy` status is unaffected by an all-exhausted agent resolution (this is informational, not a health-gating check, unless the brief's intent is otherwise - default to informational-only per "should report... so this is inspectable," not "should fail doctor").

  **QA Scenarios**:
  ```
  Scenario: doctor reports resolved agent (human)
    Tool: bash
    Steps: run `made doctor` in a fixture repo with agent: auto, agents: [claude], claude present+authed
    Expected: stdout contains a line naming "claude" as the resolved agent
    Evidence: evidence/task-13-doctor-human.txt

  Scenario: doctor reports all-exhausted (JSON)
    Tool: bash + jq
    Steps: run `made doctor --json` in a fixture repo where every candidate is missing
    Expected: JSON output's agent-resolution key shows selected absent and one attempt per configured candidate with reason "missing"
    Evidence: evidence/task-13-doctor-json-exhausted.txt
  ```

  **Commit**: YES | Message: `feat(cli): made doctor reports resolved/attempted agent` | Files: `cmd/made/doctor.go`, `cmd/made/doctor_test.go`

- [ ] 14. Human-readable resolution summary line (D20)

  **What to do**: Wherever the daemon pipeline and `made verify` already print a human-mode stage failure/summary (identify the exact existing call site(s) during implementation - likely near where `StageResult.Message`/`Error` is already rendered for a human consumer), add one line summarizing the `AgentResolution` from Task 10 - on success, e.g. "review agent: claude (resolved)"; on all-exhausted, e.g. "no review agent available: codex (missing), claude (quota-exhausted until 2026-09-10T06:00:00Z)". Reuse existing output plumbing (whatever function already writes stage summaries to stderr/stdout in human mode) rather than adding a new print path.

  **Must NOT do**: Do not add a new output subsystem/logger - extend the existing human-mode summary rendering only. Do not print this line in JSON mode (JSON already carries the structured field from Task 10).

  **Parallelization**: Can Parallel: NO (depends on 10) | Wave 3 | Blocks: none | Blocked By: 10

  **References**:
  - Wherever `internal/daemon`/`cmd/made/verify.go` already renders a human-mode stage summary (locate exact call site before implementing - do not guess a new location).

  **Acceptance Criteria**:
  - [ ] A successful auto-resolved run's human-mode output names the agent that actually ran.
  - [ ] An all-exhausted run's human-mode output lists every attempted candidate and its reason, human-readably.
  - [ ] JSON-mode output is unaffected (no duplicate/leaked line into JSON payloads).

  **QA Scenarios**:
  ```
  Scenario: human output names resolved agent
    Tool: bash
    Steps: run made (or made verify) human-mode with a successful auto-resolve
    Expected: stderr/stdout contains a line naming the resolved agent kind
    Evidence: evidence/task-14-human-summary-success.txt

  Scenario: human output lists all-exhausted reasons
    Tool: bash
    Steps: run human-mode with every candidate failing
    Expected: output lists each candidate and its specific reason, not a generic error string
    Evidence: evidence/task-14-human-summary-exhausted.txt
  ```

  **Commit**: YES | Message: `feat(cli): human-readable agent-resolution summary line` | Files: identified call site(s) + `_test.go`

- [ ] 15. `AGENTS.md` documentation update (D21)

  **What to do**: Add one concise entry to this repo's `AGENTS.md` (matching its existing dense, pointer-heavy style - see the file's own "Maintaining this file" section) describing: `agent: auto`/empty + `agents: [...]` semantics and precedence (D11), that an explicit non-`auto` `agent:` still means "only this one, no fallback," the `git push -o agent=<kind>` preference and its `AllowRepoCommands` trust gating (D4), and a pointer to `internal/agent/resolve.go` as the authoritative source rather than restating its logic. Keep it to the same density/length as this file's existing entries - one paragraph, pointer-first.

  **Must NOT do**: Do not create a new `docs/*.md` file (D21). Do not restate implementation detail already visible by reading `resolve.go` - point to it.

  **Parallelization**: Can Parallel: NO (describes the final shape, do last) | Wave 3 | Blocks: none | Blocked By: 1, 6, 11, 12, 13

  **References**: `AGENTS.md`'s existing entries (this repo's own committed file) as the style precedent.

  **Acceptance Criteria**:
  - [ ] New entry present, one paragraph, matches existing density/style.
  - [ ] Entry points at `internal/agent/resolve.go` and `internal/config/config.go`'s `AgentCandidates`/`AgentIsPinned` rather than re-explaining their internals.

  **QA Scenarios**:
  ```
  Scenario: entry readable and accurate
    Tool: manual read
    Steps: read the new AGENTS.md paragraph against the actually-implemented code
    Expected: every claim in the paragraph matches the shipped implementation exactly (no drift)
    Evidence: evidence/task-15-agents-md-accuracy.txt (diff/quote comparison)
  ```

  **Commit**: YES | Message: `docs(agents): document agent auto-resolve and push-option preference` | Files: `AGENTS.md`

- [x] 16. Config precedence + validator tests (companion to Task 1, D1/D11)

  **What to do**: Table-driven tests in `internal/config/config_test.go` (or a new `config_agent_resolve_test.go` matching this repo's per-topic test-file convention) covering every precedence branch of D11 as pure config-layer assertions (no probing/resolution involved yet, that's Task 6's job): explicit pinned `agent:` ignores `agents:` entirely; `agent: auto`/`""` + non-empty `agents:` returns that list in order; `agent: auto`/`""` + empty `agents:` returns `SupportedKinds()`'s order; `review.required: true` with each of the above passes `Validate()` (D1); an explicit invalid `agent:` value still fails `Validate()` (regression-proves D1 didn't over-relax).

  **Must NOT do**: Do not duplicate Task 1's acceptance-criteria tests if they already cover a case - this task fills in any precedence-matrix cells Task 1 didn't already write (RED-then-GREEN discipline: write the table first, confirm each row fails before Task 1's implementation, confirm all pass after).

  **Parallelization**: Can Parallel: YES (test-writing can start alongside Task 1's implementation) | Wave 1 | Blocks: none | Blocked By: none (co-developed with Task 1)

  **References**: `internal/config/config_test.go`/`config_extended_test.go` existing table-driven style.

  **Acceptance Criteria**:
  - [ ] Every precedence branch in D11 has one explicit table row.
  - [ ] `go test ./internal/config/...` green after Task 1 lands.

  **QA Scenarios**:
  ```
  Scenario: full precedence matrix
    Tool: go test
    Steps: table test with 5 rows: pinned+agents-ignored, auto+agents-ordered, auto+empty-agents-default-order, required+auto-passes, invalid-agent-still-fails
    Expected: all 5 rows pass after Task 1
    Evidence: evidence/task-16-precedence-matrix.txt
  ```

  **Commit**: YES | Message: `test(config): cover agent:auto/agents: precedence matrix` | Files: `internal/config/config_agent_resolve_test.go`

- [ ] 17. Mid-run fallback + all-candidates-exhausted integration tests (companion to Tasks 6, 7, 8)

  **What to do**: End-to-end tests (using Task 2's fleet harness) at the `reviewStage()` and `Runner.Run` levels proving the full RED-then-GREEN arc for each of the 3 independent failure reasons plus their combinations: (a) missing-only skip, (b) unauthenticated-only skip, (c) quota-exhausted-only skip, (d) mixed reasons across a 3+ candidate list, (e) mid-run `ErrAgentCapacity` fallback to next candidate succeeding, (f) all candidates exhausted producing the structured failure (not a generic error) with every reason correctly attributed. Write each as a failing test first (RED) against pre-Task-6/7/8 code conceptually, then confirm GREEN once those tasks land - if those tasks are already merged by the time this task starts, still write the test to fail against a deliberately-reverted/stubbed resolver first to prove the test isn't vacuously passing, per this task's own RED/GREEN requirement.

  **Must NOT do**: Do not test cursor/grok's auth behavior (D2 - no such thing exists) - only test their presence-only path.

  **Parallelization**: Can Parallel: NO (depends on 6, 7, 8) | Wave 4 | Blocks: F1-F4 | Blocked By: 6, 7, 8

  **References**: Tasks 2, 6, 7, 8's fixtures/helpers.

  **Acceptance Criteria**:
  - [ ] All 6 listed scenarios (a-f) pass at both the `reviewStage()` and `Runner.Run` levels.
  - [ ] Each test's failure-reason attribution in the resulting `AgentResolution` is asserted exactly (not just "an error occurred").

  **QA Scenarios**:
  ```
  Scenario: 3-candidate mixed-reason exhaustion
    Tool: go test
    Steps: agents:[codex,claude,cursor], codex missing, claude unauthenticated, cursor present-but-capacity-fails at spawn time
    Expected: AgentResolution.Attempts == [{codex,missing},{claude,unauthenticated},{cursor,?}] matching whichever step cursor actually failed at (presence-only means cursor would be *selected* then fail live - assert the resulting run failure reflects a live capacity failure on cursor specifically, since D2 means cursor has no pre-probe auth step to skip it earlier)
    Expected: run ultimately fails with the structured all-exhausted shape naming all 3
    Evidence: evidence/task-17-mixed-reason-exhaustion.txt
  ```

  **Commit**: YES | Message: `test(agent): cover mid-run fallback and all-candidates-exhausted scenarios` | Files: `internal/orchestrator/workfunc_resolve_test.go`, `internal/managed/runner_resolve_test.go`

- [ ] 18. Push-option + offline-queue tests (companion to Tasks 11, 12)

  **What to do**: Cover, at the `internal/gitgate` + `internal/daemon` level (matching `hook_test.go`'s existing pattern): push-option round-trip end-to-end through a real bare gate + real `post-receive` execution (not just unit-level flag parsing), trust-boundary gating (D4, both `AllowRepoCommands` true and false), and the offline-queue drop (D6) as an explicit assertion.

  **Must NOT do**: Do not skip the "against an old (pre-change) gate, `-o` behavior" scenario if it's feasible to construct in a test (init a bare repo the old way, i.e. without the new `advertisePushOptions` config, then apply only the D5 self-heal path and confirm it recovers) - this is the scenario Metis specifically flagged as a rollout hazard.

  **Parallelization**: Can Parallel: NO (depends on 11, 12) | Wave 4 | Blocks: F1-F4 | Blocked By: 11, 12

  **References**: `internal/gitgate/hook_test.go` existing pattern.

  **Acceptance Criteria**:
  - [ ] Push-option round-trip test passes against a freshly-init'd gate.
  - [ ] Self-heal test passes against a simulated pre-change (no `advertisePushOptions`) gate.
  - [ ] Trust-boundary gating test passes both directions (D4).
  - [ ] Offline-queue-drop test passes (D6).

  **QA Scenarios**:
  ```
  Scenario: self-heal recovers an old gate
    Tool: bash + git
    Steps: create a bare gate without receive.advertisePushOptions set (simulating pre-change init), trigger whatever D5 self-heal call site was chosen in Task 11, then push -o agent=claude
    Expected: push succeeds (no "receiving end does not support push options" error)
    Evidence: evidence/task-18-selfheal.txt
  ```

  **Commit**: YES | Message: `test(gitgate,daemon): cover push-option trust gating, self-heal, offline-queue drop` | Files: `internal/gitgate/hook_test.go`, `internal/daemon/daemon_test.go`

- [ ] 19. Doctor + evidence-namespacing tests (companion to Tasks 9, 13)

  **What to do**: Cover Task 9's evidence-namespacing behavior and Task 13's doctor reporting with dedicated tests if not already fully covered by those tasks' own acceptance criteria (check for gaps before writing - do not duplicate).

  **Must NOT do**: Do not skip this if Task 9/13 QA scenarios already fully cover it - in that case, mark this task's acceptance criteria as satisfied by cross-reference and skip redundant test-writing (note this explicitly in the commit message or PR body rather than writing no-op tests).

  **Parallelization**: Can Parallel: YES | Wave 4 | Blocks: F1-F4 | Blocked By: 9, 13

  **References**: Tasks 9, 13.

  **Acceptance Criteria**:
  - [ ] Every acceptance criterion in Tasks 9 and 13 has a passing test, confirmed by re-running `go test ./...` and checking coverage of those specific files.

  **QA Scenarios**:
  ```
  Scenario: gap-check
    Tool: go test -run
    Steps: run only the tests named in Tasks 9/13's QA Scenarios, confirm each passes in isolation
    Expected: all pass
    Evidence: evidence/task-19-gap-check.txt
  ```

  **Commit**: NO (verification-only task; commit folded into Tasks 9/13 if new tests were actually needed) | Message: n/a | Files: n/a

## Final Verification Wave (MANDATORY - after ALL implementation tasks)
- [ ] F1. Plan Compliance Audit - every task's acceptance criteria met, every `Must NOT Have` respected (spot-check `.made.yaml` untouched, no new `docs/*.md`, no new `Outcome` value, no new config knob beyond `agent`'s existing fields).
- [ ] F2. Code Quality Review - `golangci-lint run` clean; no dead code left behind (e.g. confirm `AgentKind()` callers are all migrated, no orphaned old call site remains); D7/D2/D9's documented-limitation comments are actually present in the shipped code (not just this plan).
- [ ] F3. Real Manual QA - run the full acceptance-criteria list from Tasks 1-19 as executable commands one more time in a clean checkout of this branch; capture a final consolidated evidence bundle.
- [ ] F4. Scope Fidelity Check - re-read `Must NOT Have` against the actual diff (`git diff main...HEAD --stat`) and confirm no out-of-scope file was touched (`.made.yaml`, `docs/*.md`, `cmd/made/runcommands.go`'s capabilities schema, `internal/cursor/doctor.go`).

## Commit Strategy
One commit per task (19 implementation/test commits, Task 19 folds into 9/13 if no new tests were needed), each buildable/testable in isolation on top of the prior commits in wave order. No squashing across tasks. Final PR is opened from the last commit on `cs/made-agent-resolve` per this task's direct-PR delivery contract.

## Success Criteria
- Every acceptance criterion across Tasks 1-19 passes.
- `go build ./...`, `go vet ./...`, `golangci-lint run`, and the full `go test ./...` (with the repo's `GIT_CONFIG_*` gpgsign workaround) are green.
- A pinned `agent: <kind>` config's behavior is provably unchanged (regression tests in Tasks 7, 8 pass).
- `agents.md`'s new section accurately describes the shipped behavior (Task 15's QA scenario).
- This repo's own `.made.yaml` is untouched.
