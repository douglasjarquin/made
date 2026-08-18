# Phase 4 runtime and security audit

This audit covers the Made source candidate
`910fc54a98e7da644bc5e170281fd935e429692f`.

The exact merge-base is
`3e19ed9d598a68149da5a73949533e8095ca4403`.

No shared Made daemon, real gate, real project, default branch, remote branch,
or unrelated worktree was used.

## Hypotheses and counterfactuals

### A: durable lifecycle state could be lost or replayed incorrectly

The initiating trigger would be cancellation, restart, a torn final WAL append,
or WAL growth during queued and awaiting-merge runs.

The masking condition would be a single live daemon process with no queue
cancellation, restart, or persistence-boundary exercise.

The visible symptom would be a queued run starting after cancellation, an
awaiting-merge run becoming terminal, a torn record aborting recovery, or an
unbounded WAL retaining every intermediate snapshot.

Command:

```text
env SSH_AUTH_SOCK= GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_SYSTEM=/dev/null go test -race ./internal/daemon -run "Test(RunManager_CancelQueuedRunNeverStartsWork|RunManager_RestoresDurableSnapshotAfterRestart|RunManager_IgnoresTornFinalWALRecord|RunManager_WALRetentionIsBounded|ReviewDecisions_RestoreAndRejectConflict)" -count=5
```

Exit code: `0`.

Relevant output: `ok github.com/douglasjarquin/made/internal/daemon 13.040s`.

Counterfactual result: the focused race suite passed, including queued
cancellation, exact restart recovery, torn-tail tolerance, retention bounds,
and first-wins decision conflict behavior.

### B: concurrent evidence publication could lose one run

The initiating trigger would be concurrent writers racing on the orphan
evidence branch reference.

The masking condition would be serialized pipeline execution or a single
writer test.

The visible symptom would be one writer failing its compare-and-swap update or
one completed run missing from the retained evidence history.

Command:

```text
env SSH_AUTH_SOCK= GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_SYSTEM=/dev/null go test -race ./internal/evidence -run TestOrphanBranchStore_ConcurrentWritesRetainBothRuns -count=10
```

Exit code: `0`.

Relevant output: `ok github.com/douglasjarquin/made/internal/evidence 3.733s`.

Counterfactual result: both concurrent writers were retained across ten race
repetitions.

### C: strict external fakes could mask unsupported invocations

The initiating trigger would be an obsolete GitHub command, a PR URL passed to
a workflow-run operation, an unsupported Claude path, or malformed Codex
structured output.

The masking condition would be permissive process fakes that ignore arguments
and accept arbitrary output.

The visible symptom would be local tests passing while a real external tool
rejects the invocation or returns an ambiguous result.

Command:

```text
env SSH_AUTH_SOCK= GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_SYSTEM=/dev/null go test ./internal/agent ./internal/github ./internal/pipeline/ci ./internal/pipeline/review -run "Test(Spawn_|StrictFakeGH|PRChecks|Run_)" -count=3
```

Exit code: `0`.

Relevant output: `ok` for `internal/agent`, `internal/github`,
`internal/pipeline/ci`, and `internal/pipeline/review`.

Counterfactual result: the strict fake suites accepted only the supported
GitHub and Codex contracts and rejected obsolete or invalid boundaries.

### D: reviewer auto-fix could stage unrelated files

The initiating trigger would be an auto-fixable reviewer patch in a worktree
that also contains unrelated modifications.

The masking condition would be a clean fixture containing only the patch.

The visible symptom would be an auto-fix commit containing files outside the
review patch.

Command:

```text
if rg -n "git add -A|git add --all|git add \\." internal/pipeline/review; then exit 1; else printf "%s\\n" "no broad reviewer staging invocation"; fi
```

Exit code: `0`.

Relevant output: `no broad reviewer staging invocation`.

Counterfactual result: reviewer containment uses the indexed patch file set
and has no broad staging invocation.

### E: public lifecycle boundary could expose obsolete or ambiguous status

The initiating trigger would be a caller using the removed global status
command or omitting the exact run identity.

The masking condition would be an in-process test that bypasses the CLI and
socket boundary.

The visible symptom would be a global-latest lookup, an invented run mutation,
or an obsolete command silently succeeding.

Command:

```text
rg -n "status is obsolete|run status" cmd/made
```

Exit code: `0`.

Relevant output includes
`made: status is obsolete; use made run status <exact-run-id>` and the exact
`made run status` handler paths.

Counterfactual result: public status requires an exact run ID and the obsolete
global command rejects with exit code 2, as proven by the disposable binary
scenario in `evidence/phase-4-manual-qa.md`.

## Final local validation observed at this source candidate

Command sequence:

```text
env SSH_AUTH_SOCK= GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_SYSTEM=/dev/null git diff --check
env SSH_AUTH_SOCK= GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_SYSTEM=/dev/null go build ./...
env SSH_AUTH_SOCK= GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_SYSTEM=/dev/null go test -race -shuffle=on -count=1 ./...
env SSH_AUTH_SOCK= GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_SYSTEM=/dev/null go vet ./...
env SSH_AUTH_SOCK= GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_SYSTEM=/dev/null golangci-lint run ./...
```

Exit code: `0` for the sequence.

Relevant output: every package completed with `ok`, and `golangci-lint`
reported `0 issues`.

Changed-file LSP diagnostics were requested for all 50 changed Go files with
severity `all`.

Result: `No diagnostics found` for every checked file.

The review-work lanes and final ledger update remain separate final-delivery
receipts and are bound to the same exact source SHA.

## Follow-up RED-to-GREEN results at the 604 source candidate

The three follow-up RED tests were fixed in Made and rerun at source candidate
`60420902ea5b1ed434f57c86ebb0e85be7be5281`.

Command:

```text
env SSH_AUTH_SOCK= GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_SYSTEM=/dev/null go test -race ./cmd/made -run TestReview_MultipleFindingsInOneStageUseOneDecision -count=5
```

Exit code: `0`.

Relevant output: `ok github.com/douglasjarquin/made/cmd/made 1.981s`.

Command:

```text
env SSH_AUTH_SOCK= GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_SYSTEM=/dev/null go test -race ./internal/evidence -run TestInRepoStoreRejectsSymlinkedEvidenceDirectory -count=5
```

Exit code: `0`.

Relevant output: `ok github.com/douglasjarquin/made/internal/evidence 1.215s`.

Command:

```text
env SSH_AUTH_SOCK= GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_SYSTEM=/dev/null go test -race ./internal/daemon -run "TestRunManager_(UpdateStagesRollsBackOnPersistenceFailure|FailsRunWhenFinalPersistenceFails)" -count=5
```

Exit code: `0`.

Relevant output: `ok github.com/douglasjarquin/made/internal/daemon 1.313s`.

The strict external boundary rerun also exited `0` for

```text
go test ./internal/agent ./internal/github ./internal/pipeline/ci ./internal/pipeline/review -run "Test(Spawn_|StrictFakeGH|PRChecks|Run_)" -count=3
```

The four package results were `ok`.

The reviewer containment source check exited `0` with
`no broad reviewer staging invocation`.

The managed gate-path boundary was also exercised by the focused command

```text
go test ./cmd/made -run 'TestGateAdmitPushRPC_(ValidBareRepoAdmitted|RejectsBareRepoOutsideMadeHome)|TestGateAdmitPushCLI_ValidGateExitsZero' -count=1
```

which exited `0`.

The final received-ref equality boundary was exercised with the strict
disposable gate fixture.
The counterfactual RED and restored GREEN receipts are recorded in
`evidence/phase-4-red-followups.md`.

Command:

```text
env SSH_AUTH_SOCK= GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_SYSTEM=/dev/null go test -race ./cmd/made -run 'TestGateNotifyPushRPC_(NormalFeatureBranchPushCreatesRun|RejectsNewSHAThatIsNotTheReceivedRef|RejectsExistingUnrelatedSHA|SupersededPushValidatesNewestSHA)' -count=5
```

Exit code: `0`.

Relevant output: `ok github.com/douglasjarquin/made/cmd/made 10.699s`.

The follow-up lifecycle boundary checks at the same exact source candidate
also exited `0`:

```text
env SSH_AUTH_SOCK= GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_SYSTEM=/dev/null go test -race ./internal/daemon -run 'TestRunManager_(CancelSpooledQueuedRunTransitionsTerminal|CancelQueuedRunNeverStartsWork|CloseDoesNotDiscardConcurrentDurableMutation|FailsRunWhenFinalPersistenceFails|OpenRunManager_PreservesStateAfterRecoveryFailure)' -count=5
ok github.com/douglasjarquin/made/internal/daemon 2.932s

env SSH_AUTH_SOCK= GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_SYSTEM=/dev/null go test ./cmd/made -run TestGateNotifyPushRPC_RejectsStaleAncestorSHA -count=1
ok github.com/douglasjarquin/made/cmd/made 1.022s

env SSH_AUTH_SOCK= GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_SYSTEM=/dev/null go test ./internal/daemon -run TestRunManager_CancelSpooledQueuedRunTransitionsTerminal -count=1
ok github.com/douglasjarquin/made/internal/daemon 0.471s

env SSH_AUTH_SOCK= GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_SYSTEM=/dev/null go test ./internal/daemon -run TestRunManager_CloseDoesNotDiscardConcurrentDurableMutation -count=1
ok github.com/douglasjarquin/made/internal/daemon 0.297s
```

The real binary scenario in `evidence/phase-4-manual-qa.md` additionally
proved public spooled cancellation and durable terminal-state recovery after
graceful daemon restart.

The final review-agent environment boundary also exited `0`:

```text
env SSH_AUTH_SOCK= GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_SYSTEM=/dev/null go test -race ./internal/agent -run 'TestSpawn_(CodexUsesStructuredExecContract|DoesNotPassSensitiveEnvironmentToCodex|RejectsStructuredOutputWithoutFindingsField|ParsesFindingsFromFakeAgent|NonZeroExitReturnsError|LogsInvocation)' -count=5
ok github.com/douglasjarquin/made/internal/agent 1.971s
```

The allowlist source fix is committed at
`910fc54a98e7da644bc5e170281fd935e429692f`.


## Final boundary audit at source candidate 910fc54

The following focused checks all exited `0` at the final source candidate
`910fc54a98e7da644bc5e170281fd935e429692f`:

```text
env SSH_AUTH_SOCK= GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_SYSTEM=/dev/null go test -race ./internal/daemon -run 'Test(RunManager_CancelQueuedRunNeverStartsWork|RunManager_CancelSpooledQueuedRunTransitionsTerminal|RunManager_RestoresDurableSnapshotAfterRestart|RunManager_IgnoresTornFinalWALRecord|RunManager_WALRetentionIsBounded|ReviewDecisions_RestoreAndRejectConflict|RunManager_FindSubmissionDoesNotCrossRepositoryBoundary|ReviewDecisions_RejectsDecisionWithoutPendingFinding|RunManager_CloseDoesNotDiscardConcurrentDurableMutation|RunManager_FailsRunWhenFinalPersistenceFails|OpenRunManager_PreservesStateAfterRecoveryFailure)' -count=5
ok github.com/douglasjarquin/made/internal/daemon 13.774s

env SSH_AUTH_SOCK= GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_SYSTEM=/dev/null go test -race ./internal/evidence -run TestOrphanBranchStore_ConcurrentWritesRetainBothRuns -count=10
ok github.com/douglasjarquin/made/internal/evidence 2.472s

env SSH_AUTH_SOCK= GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_SYSTEM=/dev/null go test ./internal/agent ./internal/github ./internal/pipeline/ci ./internal/pipeline/review -run 'Test(Spawn_|StrictFakeGH|PRChecks|Run_)' -count=3
ok github.com/douglasjarquin/made/internal/agent 1.760s
ok github.com/douglasjarquin/made/internal/github 2.326s
ok github.com/douglasjarquin/made/internal/pipeline/ci 9.152s
ok github.com/douglasjarquin/made/internal/pipeline/review 4.995s

env SSH_AUTH_SOCK= GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_SYSTEM=/dev/null go test -race ./cmd/made -run 'TestGateNotifyPushRPC_(NormalFeatureBranchPushCreatesRun|RejectsNewSHAThatIsNotTheReceivedRef|RejectsExistingUnrelatedSHA|RejectsStaleAncestorSHA|SupersededPushValidatesNewestSHA)' -count=5
ok github.com/douglasjarquin/made/cmd/made 12.831s

env SSH_AUTH_SOCK= GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_SYSTEM=/dev/null go test -race ./internal/agent -run 'TestSpawn_(CodexUsesStructuredExecContract|DoesNotPassSensitiveEnvironmentToCodex|RejectsStructuredOutputWithoutFindingsField|ParsesFindingsFromFakeAgent|NonZeroExitReturnsError|LogsInvocation)' -count=5
ok github.com/douglasjarquin/made/internal/agent 1.971s

env SSH_AUTH_SOCK= GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_SYSTEM=/dev/null go test -race ./internal/github -run TestPRChecks_RejectsEmptySuccessfulPayload -count=5
ok github.com/douglasjarquin/made/internal/github 1.862s
```

The reviewer containment source check also exited `0` with
`no broad reviewer staging invocation`.
