# Phase 4 conflict-repair continuation evidence

This receipt records the Made-only conflict repair for PR [#2](https://github.com/douglasjarquin/made/pull/2).

## Custody and ancestry

The exact requested base is `3e19ed9d598a68149da5a73949533e8095ca4403`.

The merged `origin/main` parent is `34d44be504291482d973c65bd427ba964df5e0e9`.

The pre-merge continuation tip is `25df7116bb0eebc6070603e1e080850dc9f0d211`.

The conflict-repair merge commit is `0a7c21d6d3001b85b38330766e01980bd5e92f2c`.

The review-helper cleanup commit is `bac8ed2777f584d98eb1ba8015cf1269d01a8c1e`.

The final durability correction commit is `918da271aa9521d292bbda22a862591b770f9af6`.

The final branch retains the exact base as an ancestor and retains both continuation and `origin/main` as merge parents.

The prior dirty remediation worktree was not opened, reused, cleaned, reset, deleted, copied, or inspected.

The shared Made daemon was not started, stopped, restarted, or updated.

## Conflict resolution

The exact merge command was `git merge --no-commit --no-ff origin/main`.

Mainline PR1 daemon, gate-spool, review-worktree, and modern run-command architecture was retained where it replaced obsolete duplicate CLI paths.

Continuation contracts were retained for exact GitHub check fields and workflow run IDs, strict Codex structured invocation, durable run state, review decisions, status ordering, and evidence publication.

The obsolete duplicate files `cmd/made/capabilities.go`, `cmd/made/pr.go`, `cmd/made/run.go`, and `cmd/made/run_handlers.go` were removed because their modern replacements are `cmd/made/runcommands.go`, `cmd/made/runhandlers.go`, and `cmd/made/strictjson.go`.

The merge had no unresolved paths before commit `0a7c21d6d3001b85b38330766e01980bd5e92f2c`.

## Trigger, masking condition, and visible symptom

The status trigger was a real failing `review` stage without an explicit current-stage field.

The status masking condition was deriving from the normalized stage list instead of the actual snapshot order.

The status symptom was `current_stage` reported as `rebase` instead of `review`.

The awaiting-review restart trigger was a durable `awaiting_review` snapshot at daemon reopen.

The restart masking condition was reconciling only `running` records.

The restart symptom was a non-terminal awaiting-review record surviving without durable restart failure.

The decision trigger was a first decision for a persisted awaiting-review finding.

The decision masking condition was a manager guard accepting only `running` state.

The decision symptom was a valid restored decision rejected as a state conflict.

The ID trigger was two fresh `RunManager.NewRunID` calls.

The ID masking condition was the order-derived `run-1`, `run-2` counter.

The ID symptom was a restart-reusable non-UUID identity.

The evidence trigger was two concurrent writers publishing different run directories to one ref.

The evidence masking condition was a single compare-and-swap attempt with no retry.

The evidence symptom was one run directory missing from the evidence branch.

The reviewer trigger was a valid auto-fix while unrelated user work existed in the worktree.

The reviewer masking condition was the old clean-worktree requirement and broad index mutation path.

The reviewer symptom was refusal of valid review or unrelated files entering an auto-fix commit.

The compaction trigger was the WAL record that crossed the compaction threshold.

The compaction masking condition was compacting from the old in-memory run snapshot before installing the durable candidate.

The compaction symptom was a restart restoring stage message `before` instead of `compaction-trigger`.

## RED evidence

The pre-fix focused command `env SSH_AUTH_SOCK= GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_SYSTEM=/dev/null GIT_CONFIG_NOSYSTEM=1 GIT_TERMINAL_PROMPT=0 go test ./... -count=1` exited `1` for status, daemon recovery and IDs, orphan CAS, strict review fixtures, and reviewer dirty-worktree contracts.

The post-merge lint RED command `env SSH_AUTH_SOCK= GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_SYSTEM=/dev/null GIT_CONFIG_NOSYSTEM=1 GIT_TERMINAL_PROMPT=0 make lint` exited `2` for three unused helpers: `decodeFindings`, `requireCleanWorktree`, and `statusPaths`.

The compaction RED command `env SSH_AUTH_SOCK= GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_SYSTEM=/dev/null GIT_CONFIG_NOSYSTEM=1 GIT_TERMINAL_PROMPT=0 go test ./internal/daemon -run '^TestRunManager_CompactionPersistsTriggeringTransition$' -count=1` exited `1` with `compaction lost triggering transition` and restarted message `before`.

The complete earlier external-tool RED matrix is preserved in `evidence/phase-1-red-made-remediation-continuation.md`.

## GREEN evidence

The focused status command `env SSH_AUTH_SOCK= GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_SYSTEM=/dev/null GIT_CONFIG_NOSYSTEM=1 GIT_TERMINAL_PROMPT=0 go test ./cmd/made -run 'TestStatusJSONReportsCurrentStageFromOrderedState|TestStatusJSON_ReflectsRealStageUpdate' -count=1` exited `0`.

The focused daemon command `env SSH_AUTH_SOCK= GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_SYSTEM=/dev/null GIT_CONFIG_NOSYSTEM=1 GIT_TERMINAL_PROMPT=0 go test ./internal/daemon -count=1` exited `0`.

The focused orphan command `env SSH_AUTH_SOCK= GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_SYSTEM=/dev/null GIT_CONFIG_NOSYSTEM=1 GIT_TERMINAL_PROMPT=0 go test ./internal/evidence -run 'TestOrphanBranchStore_ConcurrentWritesRetainBothRuns' -count=1` exited `0`.

The focused reviewer and agent command `env SSH_AUTH_SOCK= GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_SYSTEM=/dev/null GIT_CONFIG_NOSYSTEM=1 GIT_TERMINAL_PROMPT=0 go test ./internal/pipeline/review ./internal/agent -count=1` exited `0`.

The compaction GREEN command `env SSH_AUTH_SOCK= GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_SYSTEM=/dev/null GIT_CONFIG_NOSYSTEM=1 GIT_TERMINAL_PROMPT=0 go test ./internal/daemon -run '^TestRunManager_CompactionPersistsTriggeringTransition$' -count=1` exited `0`.

The compaction race command `env SSH_AUTH_SOCK= GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_SYSTEM=/dev/null GIT_CONFIG_NOSYSTEM=1 GIT_TERMINAL_PROMPT=0 go test -race ./internal/daemon -run '^TestRunManager_CompactionPersistsTriggeringTransition$' -count=1` exited `0`.

The final ordinary suite, race and shuffle suite, build, vet, configured lint, and formatting commands all exited `0` at final SHA `918da271aa9521d292bbda22a862591b770f9af6`.

## Manual QA boundary

The final real-binary disposable-home scenario and cleanup receipt are recorded in `evidence/phase-4-manual-qa.md`.

It observed capabilities JSON, explicit obsolete-status rejection, a disposable local daemon start/status/list/stop lifecycle, exact-ID not-found failure, and absent socket and lock after stop.

No real project, gate, pipeline, default branch, shared daemon, remote deletion, merge, auto-merge, or ask-user finding was used.

The separate review suggestion to invoke `make lint all` is not the repository-configured lint command and is not a brief requirement; the configured `make lint` target passed.

## Final direct-PR delivery read

The branch was pushed only to `origin/cs/made-remediation-continuation` at `12b83a6649b5e198049754f1cb6427d7b0dc51a0`.

The hosted `build-test-lint` check for that exact head completed successfully as check run `95537594230`.

The final read-only PR state is `open`, `merged=false`, `head=cs/made-remediation-continuation`, `head_sha=12b83a6649b5e198049754f1cb6427d7b0dc51a0`, `base=main`, `base_sha=34d44be504291482d973c65bd427ba964df5e0e9`, `mergeable=true`, `mergeable_state=clean`, and `auto_merge=null`.

The PR base is the GitHub `main` branch ref, while the exact requested base is preserved as local and remote branch ancestry through the explicit task worktree and conflict-repair merge.
