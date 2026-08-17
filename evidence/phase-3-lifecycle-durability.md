# Phase 3 lifecycle and durability evidence

Base: `3e19ed9d598a68149da5a73949533e8095ca4403`

## Durable run identity and lifecycle

Command:

```text
env SSH_AUTH_SOCK= GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_SYSTEM=/dev/null go test ./internal/daemon ./cmd/made -run 'Test(RunManager|ReviewDecisions|Capabilities|StatusJSON|Doctor|Daemon)' -count=1
```

Result:

```text
ok   github.com/douglasjarquin/made/internal/daemon  2.980s
ok   github.com/douglasjarquin/made/cmd/made  7.570s
```

The run manager now returns the persisted queued identity before drain,
supports exact submission metadata and SHA fields, removes queued jobs before
execution on cancellation, preserves immutable snapshots, keeps awaiting
merge non-terminal, and records succeeded/canceled/superseded terminal states.
The WAL checkpoint test covers restart restoration, awaiting-merge to
succeeded, torn final-record tolerance, bounded WAL retention, and durable
first-wins review decisions.

## Evidence and reviewer containment

Command:

```text
env SSH_AUTH_SOCK= GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_SYSTEM=/dev/null go test ./internal/evidence -count=1
```

Result:

```text
ok   github.com/douglasjarquin/made/internal/evidence  0.991s
```

Concurrent orphan evidence writers use compare-and-swap ref updates with
bounded retries and retain both run records.
In-repository evidence uses same-directory write, fsync, rename, and directory
fsync ordering with path containment checks.
Review auto-fixes stage only the files in the applied patch through
`git apply --index`; unrelated worktree files remain outside the commit.

## Semantic configuration and rebase fixture

Command:

```text
env SSH_AUTH_SOCK= GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_SYSTEM=/dev/null go test ./internal/config ./internal/orchestrator -count=1
```

Result:

```text
ok   github.com/douglasjarquin/made/internal/config  0.468s
ok   github.com/douglasjarquin/made/internal/orchestrator  5.064s
```

YAML loading now rejects unknown fields and multiple documents.
The trusted `no_ci` switch is enforced by skipping the CI command while
recording a passing disabled stage.

Command:

```text
env SSH_AUTH_SOCK= GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_SYSTEM=/dev/null go test ./internal/pipeline/rebase -run 'TestRun_(CleanRebaseProceeds|ConflictingRebaseHalts)' -count=1 -v
```

Result:

```text
PASS
ok   github.com/douglasjarquin/made/internal/pipeline/rebase  1.030s
```

The previously observed clean-rebase failure was caused by the child Git
process lacking identity under signing isolation; Made now supplies a
gate-local identity and disables signing for that child only.
