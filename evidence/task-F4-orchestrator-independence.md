# F4 - Scope Fidelity Check: internal/orchestrator vs no-mistakes/internal/daemon (manager.go + executor.go)

**Verdict up front**: Independence holds.
made's `internal/orchestrator` package (`scaffold.go`, `workfunc.go`, `params.go`) uses its own vocabulary, its own file decomposition, and a genuinely different control-flow mechanism from no-mistakes' push-launch path in `internal/daemon/manager.go` (plus the `internal/pipeline/executor.go` it delegates step execution to). No shared identifiers, comments, or string literals were found beyond unavoidable domain terms (`worktree`, `run`, `stage`/`step`, `config`, `agent`) that any git-push-triggered validation pipeline would need regardless of authorship.

Sources read in full:
- `/Users/douglasjarquin/github/douglasjarquin/made/internal/orchestrator/scaffold.go` (171 lines)
- `/Users/douglasjarquin/github/douglasjarquin/made/internal/orchestrator/workfunc.go` (351 lines)
- `/Users/douglasjarquin/github/douglasjarquin/made/internal/orchestrator/params.go` (37 lines)
- `/Users/douglasjarquin/github/douglasjarquin/made/internal/orchestrator/scaffold_test.go`, `workfunc_test.go`, `params_test.go` (skimmed for identifier reuse)
- `/Users/douglasjarquin/github/oss/no-mistakes/internal/daemon/manager.go` (1249 lines total; read in full, with close attention to lines 543-1048, the push-receipt-to-launch path cited in the plan as manager.go:611-612 and :959-961)
- `/Users/douglasjarquin/github/oss/no-mistakes/internal/pipeline/executor.go` (function signatures for all ~50 methods, plus `Execute` read in full at lines 148-214) - the type manager.go's launched goroutine actually drives, and the closest no-mistakes analog to made's per-stage chain logic in `workfunc.go`.

Neither repo has a `.codegraph/` index, so this check was done by direct file reads and targeted greps rather than a graph query.

---

## 1. Naming

| Concept | made (`internal/orchestrator`) | no-mistakes (`internal/daemon`, `internal/pipeline`) |
|---|---|---|
| Per-run bundle of dependencies | `RunContext` (scaffold.go:22) | no equivalent struct; fields (`cfg`, `ag`, `execSteps`, `wtDir`, `run`, `repo`) stay as loose locals/closure captures inside `startRunWithIntentSource` (manager.go:733-1048) |
| Callback the caller supplies | `WorkFunc func(ctx, *RunContext) error` (scaffold.go:30) | no callback type; the goroutine body is inlined at manager.go:961-1045 |
| Setup/teardown pair | `Setup` / `Cleanup` (scaffold.go:37, :88) | no paired functions; setup is inline in `startRunWithIntentSource`, teardown is an inline deferred closure at manager.go:826-832 (pre-launch) and manager.go:965-1005 (post-launch) |
| Entry point tying setup+work+cleanup together | `Run` (scaffold.go:98) | `HandlePushReceived` (manager.go:613) delegates straight to `startRun`/`startRunWithIntentSource` - no separate "Run" wrapper exists |
| Stage-chain constructor | `NewWorkFunc` (workfunc.go:57) | no constructor of this kind; steps come from a `StepFactory` (`steps.AllSteps`, manager.go:32,79) that returns a `[]pipeline.Step` |
| Sequential-stage driver type | `chain` struct (workfunc.go:74), driven by `chain.run()` (workfunc.go:97) with 9 explicit named methods (`intentStage`, `rebaseStage`, `reviewStage`, `testStage`, `documentStage`, `lintStage`, `pushStage`, `prStage`, `ciStage`) | `Executor` struct (executor.go:44), driven by `Execute()` (executor.go:148) which **loops** over a `[]Step` interface slice (executor.go:169, :178) - no per-stage named methods exist because stages are polymorphic `Step.Execute()` implementations living in a separate `steps/` package (`ci_bitbucket.go`, `ci_gitlab.go`, `intent_prompt.go`, etc.) |
| Per-stage outcome record | `daemon.StageResult{Name, Result}` populated via `c.finish` (workfunc.go:141) | `db.StepResult`, persisted via `db.InsertStepResult`/`db.CompleteStepWithStatus` (executor.go:170-174, :185) |
| Failure wrapping after an irreversible push | `stageFailure` (workfunc.go:149), branches on `c.pushed` bool (workfunc.go:94, :150) | no equivalent single function; irreversibility isn't modeled as a boolean gate at all - failures route through `failRun`/`completeRun` (executor.go:1206, :1236) regardless of push state |
| Human-approval park point | `parkForApproval` (workfunc.go:330), backed by `daemon.ReviewDecisions.Wait` (workfunc.go:332) | `waitForApprovalOrReconcile` (executor.go:1118), `reconcileApprovalGate` (executor.go:1187), `claimGateReconciliation` (executor.go:1176), resolved later via `Executor.Respond`/`RespondWithOverrides` (executor.go:115, :122) - a materially larger, DB-backed, restart-survivable subsystem, not a same-named or same-shaped counterpart |
| Finding conversion helper | `findingsToAskUser` (workfunc.go:344) mapping `agent.Finding` -> `daemon.AskUserFinding` | no direct counterpart; findings flow through `types.Finding`/`db` finding tables inside `executor.go`'s much larger event/finding machinery (`emitStepEventWithFindings*`, `findingsCount`, `selectedFindingCount`, executor.go:1320-1438) |
| Push-event entry point | n/a in orchestrator (push handling lives in `internal/daemon` per Task 10, out of this package's scope) | `HandlePushReceived` (manager.go:613) |
| Run launch after setup | `Run`/`Setup` (scaffold.go) | `startRun` (manager.go:726) / `startRunWithIntentSource` (manager.go:733) |

No identifier in `internal/orchestrator` (`RunContext`, `WorkFunc`, `Setup`, `Cleanup`, `Run`, `NewWorkFunc`, `chain`, `stageFailure`, `parkForApproval`, `findingsToAskUser`, `stageResultPass`/`stageResultFail`, `stageNameIntent`/etc., `resolveConfig`, `extractTrustedConfig`, `derivePRTitle`, `deriveEvidenceRef`) appears anywhere in `manager.go` or `executor.go`. Conversely, no-mistakes' actual names for the same concepts (`HandlePushReceived`, `startRunWithIntentSource`, `StepFactory`, `Executor`, `Step`, `StepContext`, `StepOutcome`, `waitForApprovalOrReconcile`, `reconcileApprovalGate`, `claimGateReconciliation`, `failRun`, `completeRun`, `emitStepEvent*`, `recoveredRunPlan`) do not appear anywhere in `internal/orchestrator`. The only overlapping words are generic domain nouns unavoidable in any git-push validation pipeline: `run`, `worktree`, `config`, `agent`, `branch`.

---

## 2. File / structural layout

**made** (`internal/orchestrator/`, 4 non-test files, ~830 lines):
- `scaffold.go` (171 lines) - resource lifecycle only: cut worktree, open visibility pane, resolve trusted+pushed config, build evidence store, bundle into `RunContext`; symmetric `Setup`/`Cleanup`/`Run`.
- `workfunc.go` (351 lines) - the 9-stage chain as a hand-written sequence of `chain` methods, one per stage, each following the same 4-line shape (start -> call package func -> branch on `result.OK` -> finish/park).
- `params.go` (37 lines) - two small, pure, single-purpose derivation helpers (`derivePRTitle`, `deriveEvidenceRef`) split out from the chain specifically because they don't touch `RunContext` lifecycle or stage sequencing.

**no-mistakes** (closest analog spans two files across two packages):
- `internal/daemon/manager.go` (1249 lines) - `RunManager` is a much larger type owning subscriber fan-out (`Subscribe`, `broadcast`, `eventMailbox`), crash recovery of parked runs (`recoverableParkedRuns`, `prepareRecoveredRun`, `resumeRecoveredRun`), branch-level locking (`branchLocks`), telemetry, and eval auto-capture (`autoCaptureEvalCase`), in addition to push handling. The push-launch logic itself (`startRunWithIntentSource`, manager.go:733-1048) is one ~320-line method, not split into a Setup/Cleanup pair or a dedicated file - config resolution, agent construction, worktree creation, and goroutine launch are all inlined sequentially in that single function.
- `internal/pipeline/executor.go` (1400+ lines) - the actual per-stage execution loop `Executor` is a **separate package** the daemon hands the constructed run off to. Stages themselves (`Step` implementations) live in a third package, `internal/pipeline/steps/`, one file per external concern (`ci_bitbucket.go`, `ci_gitlab.go`, `intent_prompt.go`, `pipeline_delivery.go`, `shell_*.go`, etc.) - a per-integration file split, not made's per-lifecycle-concern split.

made keeps setup, chaining, and small derivations in three flat files inside one package with no interface abstraction. no-mistakes spreads the same conceptual territory across three packages (`daemon`, `pipeline`, `pipeline/steps`) built around a polymorphic `Step` interface, a database-backed run/step-result model, and a separate reconciliation subsystem for daemon-restart recovery that made's design does not have at all (made has no persistence layer for stage state beyond the in-memory `RunManager.UpdateStages`/`UpdatePendingFindings` calls added in Task 1). These are different decompositions arrived at independently, not a relabeled copy of one another.

---

## 3. Control-flow shape

**made** (`workfunc.go:97-133`, `chain.run()`): a flat, hardcoded sequence of 9 `if err := c.xStage(); err != nil { return err }` calls, one per named stage, ending with a `prStage()`/`ciStage()` pair that thread `prResult.PRURL` through explicitly. There is no loop and no interface - the stage list is fixed at compile time by literally writing out 9 calls. Two stages (`reviewStage`, `documentStage`) contain an extra inline branch: `if len(result.PendingFindings) > 0 { c.parkForApproval(...) }` sitting between the `OK`-check and `c.finish(...)`, since a stage can be `OK` and still carry findings a human must weigh in on (workfunc.go:208-212, :243-247). Irreversibility after push is tracked with one bool (`c.pushed`, workfunc.go:94) consulted only inside `stageFailure` (workfunc.go:149-161).

**no-mistakes** (`executor.go:148-214`, `Execute()`): a `for i, step := range e.steps` loop over an interface slice. Each iteration does DB bookkeeping (`InsertStepResult` up front for every step, `CompleteStepWithStatus` for skips), checks a `ctx.Err()` cancellation gate every iteration (something made's flat sequence doesn't do stage-by-stage - made relies on the outer context passed into each package call), and can early-`break` out of the loop entirely via a `skipRemaining` signal that then marks every *remaining* step as skipped in a nested loop (executor.go:195-204) - a construct with no counterpart in made's chain, which has no "skip the rest" concept. The human-approval-park equivalent (`waitForApprovalOrReconcile`/`reconcileApprovalGate`, executor.go:1118-1205) is invoked from deep inside `executeStep` (not shown here but referenced at executor.go:562), is restart-recoverable via `recoveredGate`/`ValidateRecoveredRun` (executor.go:242, :431), and persists its decision to the database - versus made's `parkForApproval` (workfunc.go:330-342), which is a synchronous, in-memory `reviewDecisions.Wait` call with no persistence or restart-resume path (made's daemon simply keeps the process running; it does not recover parked runs across a daemon restart the way no-mistakes explicitly does via `recoverableParkedRuns`, manager.go:104).

Both arrive at conceptually similar behavior (walk N stages/steps in order, stop on failure, pause for human approval when a stage flags findings) - which the plan calls expected and fine. But the actual code shapes differ: fixed unrolled calls vs. a data-driven loop over polymorphic steps; a single bool flag for post-push irreversibility vs. no such flag; a synchronous in-package wait vs. a DB-backed, restart-recoverable reconciliation state machine spread across five-plus methods.

---

## 4. Copy-paste check

Grepped `internal/orchestrator/*.go` (including tests) against literal strings and comment phrases from `manager.go`/`executor.go`: no shared string literals (log/error message text differs entirely - e.g. made's `"orchestrator: push succeeded (branch %s now on %s), but %s failed: %s - the branch is live on the real remote, no automatic action taken"` vs. no-mistakes' `"cannot evaluate disable_project_settings: ..."` family), no shared comment phrasing, and no identifier collisions beyond generic Go/domain nouns (`run`, `worktree`, `config`, `branch`, `agent`) that are unavoidable vocabulary for this problem domain regardless of who writes the code. made's comments reference concerns absent from no-mistakes entirely (herdr-visibility panes, `.made.yml` pushed-vs-trusted split by filename, human being unable to merge a PR programmatically) and vice versa (no-mistakes' comments reference `disable_project_settings`, ACP registries, eval-corpus auto-capture, telemetry field sets - none of which exist in made at all).

---

## Verdict

Independence holds. made's `internal/orchestrator` (`scaffold.go`, `workfunc.go`, `params.go`) was independently designed: its identifiers do not match any real no-mistakes identifier for the same concept, its file split (lifecycle scaffold / stage chain / pure derivation helpers) does not mirror no-mistakes' actual layout (a large `RunManager` method in `manager.go` handing off to a separate `Executor`+`Step`-interface package plus a per-integration `steps/` package), and its control-flow shape (a flat unrolled 9-call sequence with one `pushed` bool and a synchronous in-memory park/wait) is structurally distinct from no-mistakes' data-driven loop over a polymorphic `Step` slice with DB-backed step records, skip-remainder handling, and a five-method, restart-recoverable approval-reconciliation subsystem. The two implementations reach similar high-level behavior (sequential validation stages, pause for human approval on findings, refuse to auto-revert after an irreversible push) through genuinely different code, naming, and decomposition - exactly the "independent synthesis of design" the plan's F4 principle asks for, not independent behavior arrived at by copying.
