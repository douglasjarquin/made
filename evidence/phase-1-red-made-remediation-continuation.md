# Phase 1 — captured RED evidence

All commands ran before the corresponding production fix.

Environment prefix for every command: `env SSH_AUTH_SOCK= GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_SYSTEM=/dev/null`.

## GitHub fake and client boundary

Command: `go test ./internal/github -run 'TestStrictFakeGH' -count=1`

Exit: `1`.

Relevant output:

```text
TestStrictFakeGHRejectsUnsupportedJSONFields: strict fake accepted unsupported invocation, output={"mergeStateStatus":"CLEAN"}
TestStrictFakeGHRejectsPRURLAsWorkflowRunID: strict fake accepted PR URL as workflow run ID, output=log line 1
TestStrictFakeGHInvocationLogDoesNotAcceptLegacyMergeStateCommand: legacy merge-state invocation was accepted: invoked: args=pr view https://github.com/example/repo/pull/1 --json mergeStateStatus
```

This proves the fake accepts obsolete fields and PR URLs at the wrong boundary.

## CI check and workflow-run contract

Command: `go test ./internal/pipeline/ci -run 'TestRun_(UsesPrChecksJSONContract|PassesWorkflowRunIDToLogsAndRerun)' -count=1`

Exit: `1`.

Relevant output:

```text
TestRun_UsesPrChecksJSONContract: expected gh pr checks invocation, got invoked: args=auth status
invoked: args=pr view https://github.com/example/repo/pull/7 --json mergeStateStatus
TestRun_PassesWorkflowRunIDToLogsAndRerun: PR URL was passed to a workflow-run command: invoked: args=auth status
invoked: args=pr view https://github.com/example/repo/pull/8 --json mergeStateStatus
```

This proves the production CI stage invokes mergeability instead of the supported checks contract and cannot preserve workflow run identity.

## Agent invocation and structured output

Command: `go test ./internal/agent -run 'TestSpawn_(CodexUsesStructuredExecContract|RejectsStructuredOutputWithoutFindingsField)' -count=1`

Exit: `1`.

Relevant output:

```text
TestSpawn_CodexUsesStructuredExecContract: expected Codex structured invocation token "exec", got invoked: args=[.../fakeagent review --worktree ...]
TestSpawn_RejectsStructuredOutputWithoutFindingsField: expected schema-invalid structured output to fail closed
```

This proves the current adapter uses the wrong Codex shape and accepts an output without the required findings field.

## Run lifecycle and decision contracts

Command: `go test ./internal/daemon -run 'TestRunManager_(CancelQueuedRunNeverStartsWork|AwaitingMergeDoesNotEmitTerminalCompletion|SnapshotDoesNotAliasStageSlices)|TestReviewDecisions_FirstDecisionWins' -count=1`

Exit: `1`.

Relevant output:

```text
TestRunManager_CancelQueuedRunNeverStartsWork: cancelled queued run started execution
TestRunManager_AwaitingMergeDoesNotEmitTerminalCompletion: awaiting-merge emitted terminal event: {RunID:run-1 Kind:run_completed ...}
TestRunManager_SnapshotDoesNotAliasStageSlices: snapshot stages aliased caller memory: [{Name:intent Result:fail}]
TestReviewDecisions_FirstDecisionWins: conflicting decision overwrote first decision: got "approved"
```

These are independent trigger/masking/symptom failures in queue cancellation, awaiting-merge lifecycle, public snapshot ownership, and decision conflict rules.

## Reviewer containment

Command: `go test ./internal/pipeline/review -run 'TestRun_AutoFixDoesNotStageUnrelatedChanges' -count=1`

Exit: `1`.

Relevant output:

```text
TestRun_AutoFixDoesNotStageUnrelatedChanges: unrelated file was included in auto-fix commit: reviewed.txt
unrelated.txt
```

This proves `git add -A` crosses the reviewer containment boundary.

## Semantic configuration

Command: `go test ./internal/config -run 'TestLoadEffectiveConfig_RejectsUnknownSemanticSwitch' -count=1`

Exit: `1`.

Relevant output:

```text
TestLoadEffectiveConfig_RejectsUnknownSemanticSwitch: expected unknown semantic configuration switch to fail closed
```

This proves unknown configuration switches are silently accepted.

## Public structured command surface

Command: `go test ./cmd/made -run 'Test(CapabilitiesJSONExposesStructuredRunContract|ObsoleteStatusCommandIsRejected)' -count=1`

Exit: `1`.

Relevant output:

```text
TestCapabilitiesJSONExposesStructuredRunContract: capabilities exit code = 2; stderr=made: unknown command "capabilities"
TestObsoleteStatusCommandIsRejected: obsolete status exit code = 1, want 2; stderr=made status: daemon not reachable: dial .../daemon.sock: ... no such file or directory
```

This proves the native versioned command surface is absent and the obsolete global-latest status path is still active.

## Evidence retention and concurrent publication

Command: `go test ./internal/evidence -run 'TestOrphanBranchStore_ConcurrentWritesRetainBothRuns' -count=1`

Exit: `1`.

Relevant output:

```text
TestOrphanBranchStore_ConcurrentWritesRetainBothRuns: evidence branch missing run-a: run-b/result.json
```

This proves concurrent evidence publication loses one run under the current compare-and-swap update path.

## Current-stage public status

Command: `go test ./cmd/made -run 'TestStatusJSONReportsCurrentStageFromOrderedState' -count=1`

Exit: `1`.

Relevant output:

```text
TestStatusJSONReportsCurrentStageFromOrderedState: status omitted current stage: {"schema_version":1,"run_id":"run-current-stage",...}
```

This proves the current structured status schema cannot report the active stage after a reconnect or missed event.

## Durable restart recovery

Command: `go test ./internal/daemon -run 'TestRunManager_RestoresDurableSnapshotAfterRestart' -count=1`

Exit: `1` during test compilation.

Relevant output:

```text
undefined: OpenRunManager
undefined: RunSubmission
undefined: RunAwaitingMerge
```

This is the named public durability contract missing from the exact-base source, not a fixture or import typo: no durable manager/open path or awaiting-merge state exists yet.

## RED-to-GREEN boundary

No production source fix was applied before these RED commands completed.

The strict fake, Codex adapter, GitHub check adapter, lifecycle manager, config parser, reviewer, and CLI surface are now pinned to independent contract failures.
