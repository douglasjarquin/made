# Phase 4 follow-up RED contracts

These follow-up RED tests were written after the initial Phase 1 matrix when
the final review found additional Made-owned boundary defects.

The source candidate before the fixes was
`4617d622b8cdaeb38d2b49458459565c8e7755b7`.

## Review decision grouping

Trigger: two pending findings belong to the same review stage and the user
supplies one stage decision.

Masking condition: each stage has at most one pending finding or the test
supplies one decision per finding.

Visible symptom: the CLI sends a second decision for the same stage and exits
with `no approve/reject decision provided` or a duplicate-decision error.

Command:

```text
env SSH_AUTH_SOCK= GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_SYSTEM=/dev/null go test ./cmd/made -run TestReview_MultipleFindingsInOneStageUseOneDecision -count=1
```

Exit code: `1`.

Relevant output:

```text
--- FAIL: TestReview_MultipleFindingsInOneStageUseOneDecision
review_test.go:190: exit code = 1, want 0
stderr=made review: no approve/reject decision provided
FAIL
FAIL github.com/douglasjarquin/made/cmd/made
```

## In-repository evidence path containment

Trigger: a pre-existing symlink points the configured evidence directory outside
the repository.

Masking condition: the configured evidence path contains only ordinary
directories.

Visible symptom: `WriteEvidence` follows the symlink and writes evidence
outside the repository.

Command:

```text
env SSH_AUTH_SOCK= GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_SYSTEM=/dev/null go test ./internal/evidence -run TestInRepoStoreRejectsSymlinkedEvidenceDirectory -count=1
```

Exit code: `1`.

Relevant output:

```text
--- FAIL: TestInRepoStoreRejectsSymlinkedEvidenceDirectory
evidence_contract_test.go:63: WriteEvidence accepted a symlinked evidence directory
FAIL
FAIL github.com/douglasjarquin/made/internal/evidence
```

## Durable stage-update rollback

Trigger: a stage update occurs after the durable run store is closed or
otherwise rejects the WAL append.

Masking condition: the durable store remains writable for the whole run.

Visible symptom: `UpdateStages` returns an error but leaves the rejected stage
in the in-memory public snapshot.

Command:

```text
env SSH_AUTH_SOCK= GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_SYSTEM=/dev/null go test ./internal/daemon -run TestRunManager_UpdateStagesRollsBackOnPersistenceFailure -count=1
```

Exit code: `1`.

Relevant output:

```text
--- FAIL: TestRunManager_UpdateStagesRollsBackOnPersistenceFailure
persistence_contract_test.go:228: in-memory stage update survived persistence failure
FAIL
FAIL github.com/douglasjarquin/made/internal/daemon
```

The three failures are contract failures in Made code, not fixture, typo, or
unavailable-service failures.

## GREEN receipts

The fixes were committed in the Made source candidate
`c359423749328c7778376d16612f36424e4a576d`.

The grouped review decision test passed under five race repetitions.

The symlink containment test passed under five race repetitions.

The durable stage-update and final-persistence tests passed under five race
repetitions.

The full source validation receipt is in
`evidence/phase-4-final-validation.md`.

## Strict CLI argument validation

Trigger: a caller supplies an unsupported trailing positional argument to the
exact-ID `run status` or `run cancel` command.

Masking condition: callers use only the documented exact run ID and optional
`--json` argument.

Visible symptom: the CLI attempts a daemon call and returns a daemon error
instead of rejecting the invented invocation at the public boundary.

Commands:

```text
env SSH_AUTH_SOCK= GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_SYSTEM=/dev/null go test ./cmd/made -run 'TestRun(Status|Cancel)RejectsUnsupportedTrailingArgument' -count=1
```

Exit code: `1` before the fix.

Relevant output:

```text
run status exit code = 1, want 2
run cancel exit code = 1, want 2
```

The fix rejects unsupported positional arguments with usage exit code `2`.

## Pre-staged reviewer containment

Trigger: an unrelated file is already staged before an auto-fixable reviewer
patch is applied.

Masking condition: the worktree contains only the reviewer patch or the
unrelated file is merely untracked.

Visible symptom: the auto-fix commit includes the unrelated staged file.

Command:

```text
env SSH_AUTH_SOCK= GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_SYSTEM=/dev/null go test ./internal/pipeline/review -run TestRun_AutoFixDoesNotStageUnrelatedChanges -count=1
```

Exit code: `1`.

Relevant output: `unrelated file was included in auto-fix commit` with both
`reviewed.txt` and `unrelated.txt` in the commit path list.

## Recovery-failure custody

Trigger: a non-final corrupt WAL record causes daemon recovery to fail after a
valid checkpoint already exists.

Masking condition: recovery succeeds or a failed recovery is never inspected.

Visible symptom: the failed recovery path compacts an empty run set and
truncates the corrupt WAL, destroying the last durable checkpoint and the
diagnostic bytes.

Command:

```text
env SSH_AUTH_SOCK= GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_SYSTEM=/dev/null go test ./internal/daemon -run TestOpenRunManager_PreservesStateAfterRecoveryFailure -count=1
```

Exit code: `1`.

Relevant output: `failed recovery replaced the durable checkpoint` with an
empty `runs` array.

## Additional GREEN receipts

The reviewer containment fix now uses an isolated temporary Git index seeded
from `HEAD`, commits only the patch paths, and restores the original index for
those paths so unrelated staged work remains staged but uncommitted.

Command:

```text
env SSH_AUTH_SOCK= GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_SYSTEM=/dev/null go test ./internal/pipeline/review -run TestRun_AutoFixDoesNotStageUnrelatedChanges -count=1
```

Exit code: `0`.

## Existing unrelated gate object containment

Trigger: a caller supplies a real commit object that exists in the managed
gate, but that object is not an ancestor of the received ref.

Masking condition: the object-existence check accepts any reachable commit and
the ref is not checked for ancestry.

Visible symptom: Made schedules a run with an input SHA that the named branch
did not receive.

The counterfactual RED proof removed the ancestry guard from the parent source
candidate `d1dab7c73c3bdf678a668891c17a04d9c34b13c4` and ran:

```text
env SSH_AUTH_SOCK= GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_SYSTEM=/dev/null go test ./cmd/made -run TestGateNotifyPushRPC_RejectsExistingUnrelatedSHA -count=1
```

Exit code: `1`.

Relevant output: `accepted existing unrelated SHA ... for feature SHA ...`.

The minimal GREEN fix adds `git merge-base --is-ancestor newSHA ref` after
verifying that the object exists and restores the guard before the GREEN run:

```text
env SSH_AUTH_SOCK= GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_SYSTEM=/dev/null go test ./cmd/made -run 'TestGateNotifyPushRPC_(RejectsExistingUnrelatedSHA|RejectsNewSHAThatIsNotTheReceivedRef|SupersededPushValidatesNewestSHA|NormalFeatureBranchPushCreatesRun)' -count=1
```

Exit code: `0`.

The strict test also preserves the existing superseded-push contract: an older
notification is rejected when the branch has advanced, while the current
received SHA still supersedes an earlier queued run.

## Stale received-ref notification

Trigger: a delayed post-receive notification names an older commit after the
same branch has advanced to a newer commit.

Masking condition: ancestry validation treats every ancestor as the current
received tip.

Visible symptom: Made schedules a run for the stale input SHA instead of
rejecting the delayed notification.

The RED command ran against the pre-fix implementation descended from
`fdd8a7853053e9eb0efc099244c5296006c3605a`:

```text
env SSH_AUTH_SOCK= GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_SYSTEM=/dev/null go test ./cmd/made -run TestGateNotifyPushRPC_RejectsStaleAncestorSHA -count=1
```

Exit code: `1`.

Relevant output: `accepted stale ancestor SHA ... for advanced feature ref`.

The GREEN fix resolves the named ref and requires its object ID to equal
`new_sha`.

```text
env SSH_AUTH_SOCK= GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_SYSTEM=/dev/null go test ./cmd/made -run TestGateNotifyPushRPC_RejectsStaleAncestorSHA -count=1
```

Exit code: `0`.

The full gate notification focused suite also passed at source commit
`60420902ea5b1ed434f57c86ebb0e85be7be5281`:

```text
env SSH_AUTH_SOCK= GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_SYSTEM=/dev/null go test ./cmd/made -run 'TestGateNotifyPushRPC_(NormalFeatureBranchPushCreatesRun|RejectsNewSHAThatIsNotTheReceivedRef|RejectsExistingUnrelatedSHA|RejectsStaleAncestorSHA|SupersededPushValidatesNewestSHA)' -count=1
```

Exit code: `0`.

## Spooled queued cancellation

Trigger: a durable `run.submit` record is queued without an attached work
function and is then canceled through the manager or public socket.

Masking condition: cancellation is tested only for a queued job already held
behind another active job.

Visible symptom: Made returns cancellation success while the exact run remains
`queued` with `execution_finished=false`.

The RED command ran against the pre-fix implementation:

```text
env SSH_AUTH_SOCK= GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_SYSTEM=/dev/null go test ./internal/daemon -run TestRunManager_CancelSpooledQueuedRunTransitionsTerminal -count=1
```

Exit code: `1`.

Relevant output: `cancelled spooled run lifecycle = ... Status:queued ... ExecutionFinished:false`.

The GREEN fix durably transitions the unattached queued record to
`canceled`, records `context.Canceled`, and sets `execution_finished=true`.

```text
env SSH_AUTH_SOCK= GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_SYSTEM=/dev/null go test ./internal/daemon -run TestRunManager_CancelSpooledQueuedRunTransitionsTerminal -count=1
```

Exit code: `0`.

## Close-versus-WAL publication ordering

Trigger: a durable mutation completes its WAL append after `Close` captures
the run list but before checkpoint compaction truncates the WAL.

Masking condition: shutdown and durable mutation are exercised sequentially,
so no append can fall between the checkpoint snapshot and WAL truncation.

Visible symptom: the update call returns success, but restart loses the
accepted stage update because `Close` compacted a stale snapshot.

The deterministic RED interleaving command ran against the pre-fix
implementation:

```text
env SSH_AUTH_SOCK= GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_SYSTEM=/dev/null go test ./internal/daemon -run TestRunManager_CloseDoesNotDiscardConcurrentDurableMutation -count=1
```

Exit code: `1`.

Relevant output: `concurrent durable mutation was lost`.

The GREEN fix serializes durable publication and close, so a mutation either
publishes before the checkpoint or fails closed after the store closes.

```text
env SSH_AUTH_SOCK= GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_SYSTEM=/dev/null go test ./internal/daemon -run TestRunManager_CloseDoesNotDiscardConcurrentDurableMutation -count=1
```

Exit code: `0` at source commit
`60420902ea5b1ed434f57c86ebb0e85be7be5281`.

## Managed gate path containment

Trigger: a socket caller submits a valid bare Git repository outside the
daemon's managed `MADE_HOME/gates/<hash>/gate.git` layout.

Masking condition: the caller uses a gate created by `made gate init` under the
current Made home.

Visible symptom: the daemon accepts the unmanaged bare repository and can
schedule the full pipeline against its remote.

Command:

```text
env SSH_AUTH_SOCK= GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_SYSTEM=/dev/null go test ./cmd/made -run TestGateAdmitPushRPC_RejectsBareRepoOutsideMadeHome -count=1
```

Exit code: `1` before the fix.

Relevant output: `gate.admitPush accepted a bare repository outside MADE_HOME`.

The fix requires an existing, non-symlinked managed gate path before either
`gate.admitPush` or `gate.notifyPush` can schedule work.

GREEN receipt at source candidate
`03f515b9aeeb8406eec0e4240ab5811fc9110943`:

```text
go test ./cmd/made -run 'TestGateAdmitPushRPC_(ValidBareRepoAdmitted|RejectsBareRepoOutsideMadeHome)|TestGateAdmitPushCLI_ValidGateExitsZero' -count=1
ok github.com/douglasjarquin/made/cmd/made 0.526s
```

## Pending-check and Codex sandbox contracts

Trigger: `gh pr checks` returns a pending check with a non-zero aggregate exit
status, or the Codex review adapter invokes `codex exec` without an explicit
read-only sandbox.

Masking condition: checks are already terminal or a permissive fake accepts
arbitrary Codex flags.

Visible symptom: Made reruns work that is still pending, or a review agent can
write to the gate worktree despite being invoked for read-only review.

Commands:

```text
env SSH_AUTH_SOCK= GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_SYSTEM=/dev/null go test ./internal/pipeline/ci -run TestRun_DoesNotRerunPendingChecks -count=1
env SSH_AUTH_SOCK= GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_SYSTEM=/dev/null go test ./internal/agent -run TestSpawn_CodexUsesStructuredExecContract -count=1
```

Both commands failed before the fixes.

The pending-check RED output reported
`pending check was rerun` with `RerunsUsed:1`.

The strict Codex fake RED output reported
`want 12 arguments, got 10`.

The GREEN runs use `bucket/state` pending detection and require
`--sandbox read-only` in the structured Codex invocation.

## Final-state publication ordering

Trigger: a run work function completes while the final WAL append is still
pending.

Masking condition: callers observe only after the persistence call returns or
the durable store never fails.

Visible symptom: a public snapshot can report `succeeded` before the final
durable record is written, then change to `failed` when persistence fails.

The final persistence failure contract is covered by
`TestRunManager_FailsRunWhenFinalPersistenceFails`.

The fix persists a candidate snapshot before replacing the live run snapshot,
so successful terminal state is not publicly visible before durable success.

The final serialized-publication fix and the pending-check/Codex sandbox fixes
are included in source candidate
`d1dab7c73c3bdf678a668891c17a04d9c34b13c4`.

## Submission, decision, check-payload, and push-identity containment

Trigger: an identical submission ID arrives for a different repository, an
approval is submitted before the review stage records a finding, a successful
GitHub check response is empty, or a gate notification names a nonexistent
commit object.

Masking condition: one repository, a pending finding, a non-empty check set,
and a real post-receive object are always used.

Visible symptom: Made returns another repository's run, pre-seeds a decision,
accepts an empty successful check payload, or schedules a forged push.

The RED commands and results were:

```text
go test ./internal/daemon -run TestRunManager_FindSubmissionDoesNotCrossRepositoryBoundary -count=1
FAIL: FindSubmission matched a submission from another repository

go test ./internal/daemon -run TestReviewDecisions_RejectsDecisionWithoutPendingFinding -count=1
FAIL: accepted a review decision without a pending finding

go test ./internal/github -run TestPRChecks_RejectsEmptySuccessfulPayload -count=1
FAIL: PRChecks accepted an empty successful payload

go test ./cmd/made -run TestGateNotifyPushRPC_RejectsNewSHAThatIsNotTheReceivedRef -count=1
FAIL: accepted forged new SHA
```

The GREEN fixes scope submission identity to repository and branch, require a
running run with a pending finding for managed decisions, reject empty or
non-successful check payloads, and require the new SHA to be a real commit
object in the managed gate.

## Review-agent environment containment

Trigger: the review agent inherits a sensitive environment variable and can
return it through findings or an error.

Masking condition: the environment contains no credential-like variable or
the fake agent ignores inherited environment.

Visible symptom: the strict fake exits after observing a test secret.

Command:

```text
go test ./internal/agent -run TestSpawn_DoesNotPassSensitiveEnvironmentToCodex -count=1
```

Exit code: `1` before the fix.

Relevant output: `fakeagent: sensitive environment was exposed`.

The GREEN adapter now filters credential-like environment keys before
launching the read-only Codex task while retaining the structured fake
contract variables.

The recovery fix leaves the checkpoint and corrupt WAL untouched when loading
fails closed.

Command:

```text
env SSH_AUTH_SOCK= GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_SYSTEM=/dev/null go test ./internal/daemon -run TestOpenRunManager_PreservesStateAfterRecoveryFailure -count=1
```

Exit code: `0`.
