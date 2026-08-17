# Phase 1 — RED contract matrix

Base under test: `3e19ed9d598a68149da5a73949533e8095ca4403`.

No production source has been edited for this phase.

## Baseline observations

Command: `go test ./...`

Exit: non-zero.

Observed symptom: disposable Git commits failed before contract execution because inherited `SSH_AUTH_SOCK` pointed at an unavailable 1Password socket.

Masking condition: the repository's test helpers inherit the host Git signing configuration.

The failure is environmental, not a Made contract result.

Command: `env SSH_AUTH_SOCK= GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_SYSTEM=/dev/null go test ./...`

Exit: non-zero.

Observed symptom: all packages passed except `internal/pipeline/rebase`, where `TestRun_CleanRebaseProceeds` returned `OK:false` with `rebase onto main halted due to conflicts in: `.

This pre-existing Made-only failure is tracked separately from the continuation matrix and must be fixed or explicitly evidenced before final validation.

## Public structured external contract

### GitHub checks

Trigger: `internal/pipeline/ci/ci.go:61` calls `github.Client.MergeableState`, which invokes `gh pr view <pr> --json mergeStateStatus`.

Masking condition: `internal/github/testdata/fakegh/main.go:57-64` returns `{"mergeStateStatus":"CLEAN"}` and ignores flags.

Visible symptom: real required checks can be pending or failed while mergeability is `CLEAN`, and Made reports CI success without inspecting checks.

RED test: `go test ./internal/pipeline/ci -run TestRun_UsesPrChecksJSON -count=1`.

RED assertion: strict fake reports the invocation is `gh pr checks <pr> --json name,state,bucket,link`, not `gh pr view ... mergeStateStatus`.

### Workflow run identity

Trigger: `ci.Run` passes the PR URL into `CheckLogs` and `RerunCheck`, which invoke `gh run view` and `gh run rerun`.

Masking condition: the fake accepts any identifier.

Visible symptom: real `gh` rejects a PR URL where a workflow run ID is required, so logs and reruns fail or are silently omitted.

RED test: `go test ./internal/pipeline/ci -run TestRun_PassesWorkflowRunIDToLogsAndRerun -count=1`.

RED assertion: strict fake rejects PR URLs and records the exact numeric workflow run ID extracted from the supported checks payload.

### Authentication and check failures

Trigger: `github.Client.run` maps all non-zero commands to generic errors and CI converts client errors into a normal failed `Result`.

Masking condition: tests exercise only successful auth and scripted check state.

Visible symptom: authentication failure is not distinguishable from an ordinary failing check at the public boundary.

RED test: `go test ./internal/github -run TestAuthStatusFailureIsExplicit -count=1`.

RED assertion: auth failure returns the typed/auth-specific error and CI does not claim a normal check result.

## Agent structured contract

### Codex invocation

Trigger: `internal/agent/spawn.go:26-30` invokes every agent as `<binary> review --worktree <path>`.

Masking condition: `internal/agent/testdata/fakeagent/main.go` ignores all arguments.

Visible symptom: installed Codex supports `codex exec --json --output-schema <file> --ephemeral -C <worktree> <prompt>`, while the current adapter sends an undocumented `review --worktree` shape.

RED test: `go test ./internal/agent -run TestSpawn_CodexUsesStructuredExecContract -count=1`.

RED assertion: strict fake requires `exec --json --output-schema <schema> --ephemeral -C <worktree> <task>` and rejects the current `review --worktree` invocation.

### Codex output

Trigger: `Spawn` unmarshals all stdout directly into `agent.Findings`.

Masking condition: fake emits a single raw JSON object with no JSONL event framing or schema check.

Visible symptom: malformed, non-final, or schema-invalid output can be mistaken for a valid review or produce an opaque parse error.

RED test: `go test ./internal/agent -run TestSpawn_RejectsInvalidStructuredOutput -count=1`.

RED assertion: output is accepted only when the final structured result matches the schema and invalid/missing fields fail closed with stdout/stderr evidence.

### Claude support boundary

Trigger: the same invocation path is used for Claude and Codex without a verified machine-readable Claude contract.

Masking condition: permissive fake treats both agent kinds identically.

Visible symptom: real Claude can enter an interactive or human-output mode that cannot be safely parsed.

RED test: `go test ./internal/agent -run TestSpawn_ClaudeUnsupportedContractIsExplicit -count=1`.

RED assertion: unsupported Claude invocation returns an explicit contract error rather than using a generic compatibility shim.

## Lifecycle and durability

### Run persistence and restart recovery

Trigger: `internal/daemon.RunManager` and `ReviewDecisions` store state only in memory.

Masking condition: daemon remains alive for the full run.

Visible symptom: status, stage results, pending findings, decisions, and awaiting-merge state disappear after daemon restart.

RED test: `go test ./internal/daemon -run TestRunManager_RestoresDurableSnapshotAfterRestart -count=1`.

RED assertion: a second manager opened on the same durable state restores the exact run ID, SHAs, stage results, decisions, errors, evidence references, and terminal/open state.

### Queued cancellation

Trigger: `RunManager.Cancel` cancels only the context and leaves the queued job in `repoQueue.pending`.

Masking condition: queued work checks cancellation before side effects.

Visible symptom: a canceled queued run later starts and can perform work.

RED test: `go test ./internal/daemon -run TestRunManager_CancelQueuedRunNeverStartsWork -count=1`.

RED assertion: canceled queued work never enters `running`, never executes its side effect, and reaches a durable canceled terminal state.

### Awaiting merge state and completion events

Trigger: `RunManager.execute` publishes `EventRunCompleted` whenever `WorkFunc` returns nil, even when `Finish` left status `running` for awaiting merge.

Masking condition: consumers poll status and ignore the event stream.

Visible symptom: consumers receive terminal completion while public status remains open/running.

RED test: `go test ./internal/daemon -run TestRunManager_AwaitingMergeDoesNotEmitTerminalCompletion -count=1`.

RED assertion: awaiting-merge emits an explicit nonterminal/open event or no terminal event, and only a true terminal transition emits completion.

### Idle and daemon-down semantics

Trigger: idle timing is driven only by event activity and public CLI status cannot distinguish daemon unreachable from idle.

Masking condition: active runs emit frequent events and callers treat socket errors as empty state.

Visible symptom: silent active work can be stopped by idle timeout, and daemon unreachability can be misreported as idle.

RED tests: `go test ./internal/daemon -run TestRun_DoesNotIdleStopWhileRunIsActiveWithoutActivityEvents -count=1` and `go test ./cmd/made -run TestStatus_DaemonUnavailableIsExplicit -count=1`.

RED assertion: active work keeps the daemon alive; unavailable socket returns a non-zero explicit error and never an idle JSON state.

### Fixed stages and current stage

Trigger: `StatusReport` exposes stage results but no current-stage field, and infrastructure errors can bypass stage result publication.

Masking condition: happy-path stage completion and a continuously connected event consumer.

Visible symptom: reconnecting callers cannot know the active stage, and failed infrastructure stages may be absent from the fixed ordered list.

RED tests: `go test ./cmd/made -run TestStatusJSON_ReportsCurrentStageAfterReconnect -count=1` and `go test ./internal/orchestrator -run TestNewWorkFunc_InfrastructureFailureRecordsFailedStage -count=1`.

RED assertion: status contains fixed ordered stages plus `current_stage`, and the active infrastructure failure is recorded with a stage-specific fail result.

### Decision timing and conflicts

Trigger: `ReviewDecisions.Set` overwrites an existing decision without checking stage/run state.

Masking condition: exactly one authorized decision arrives before the run changes state.

Visible symptom: a late approval can overwrite an earlier rejection or a decision can apply after a run has moved past the gate.

RED test: `go test ./internal/daemon -run TestReviewDecisions_RejectsConflictingDecision -count=1`.

RED assertion: first decision wins, conflicting/late decisions return an explicit conflict or stale-gate error, and decisions are keyed to exact run/stage identity.

## Evidence, configuration, and reviewer containment

### Evidence atomicity and retention

Trigger: `internal/evidence/inrepo.go:32-39` writes directly with `os.WriteFile`, and orphan evidence ref publication has no retry on compare-and-swap conflict.

Masking condition: small writes and serialized runs.

Visible symptom: torn evidence tails or lost concurrent evidence records after interruption/contention.

RED tests: `go test ./internal/evidence -run TestInRepoStore_WriteEvidenceIsAtomicOnReplacement -count=1` and `go test ./internal/evidence -run TestOrphanBranchStore_ConcurrentWritesRetainBothRuns -count=1`.

RED assertion: replacement is temp-file/fsync/rename atomic and concurrent runs retain both evidence records with bounded history/retention.

### Semantic configuration enforcement

Trigger: current trusted-config tests cover core fields but not every behavioral field's pushed-branch override or every switch at the boundary.

Masking condition: trusted and pushed fixtures use equal/default values.

Visible symptom: an untrusted branch can alter behavior if a field is accidentally read from the pushed copy or a config switch is accepted but ignored.

RED test: `go test ./internal/config -run TestLoadEffectiveConfig_RejectsPushedBehaviorOverrides -count=1`.

RED assertion: `Document`, `Review`, `DisableProjectSettings`, `NoCI`, `CI`, `Test.Evidence.Branch`, commands, agents, and `allow_repo_commands` resolve from the documented trusted source and invalid semantic switches fail closed.

### Reviewer containment

Trigger: `internal/pipeline/review/review.go:95-98` runs `git add -A` after applying an agent patch.

Masking condition: clean worktree with only the intended patch.

Visible symptom: unrelated modified/untracked files are committed as part of an auto-fix.

RED test: `go test ./internal/pipeline/review -run TestRun_AutoFixDoesNotStageUnrelatedChanges -count=1`.

RED assertion: the auto-fix commit contains only patch-authorized paths and rejects out-of-scope patches.

## Strict compatibility and live scenarios

Trigger: current fakes accept arbitrary flags and the brief requires real Made binary execution against strict Consigliere-script fakes without modifying Consigliere or the shared daemon.

Masking condition: permissive fake behavior and unit-only coverage.

Visible symptom: obsolete CLI/agent invocations pass local tests but fail at the real tool boundary.

RED test: `go test ./internal/github ./internal/agent -run 'TestStrictFakeRejects|TestSpawn_.*Contract|Test.*PrChecks' -count=1`.

RED assertion: strict fakes reject unsupported invocation shapes with non-zero status and Made reports explicit structured contract errors.

Forbidden live scenarios are not claimed: no real-project pipeline, gate initialization, run submission, default branch push, shared daemon lifecycle, merge, auto-merge, branch deletion, or ask-user decision.
