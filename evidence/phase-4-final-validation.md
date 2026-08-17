# Phase 4 final local validation evidence

The earlier ledger receipt was recorded at
`afea024e1da9f59be9181c18f18b11793a782f36`.
After the final managed-gate, cancellation, durable-publication, and review
environment corrections, the source and test validation candidate is
`910fc54a98e7da644bc5e170281fd935e429692f`.
The exact base remains
`3e19ed9d598a68149da5a73949533e8095ca4403`.

## Build

Command:

```text
env SSH_AUTH_SOCK= GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_SYSTEM=/dev/null go build ./...
```

Result: exit code 0 with no output.

## Race and shuffle suite

Command:

```text
env SSH_AUTH_SOCK= GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_SYSTEM=/dev/null go test -race -shuffle=on -count=1 ./...
```

Result: exit code 0 at source and test validation candidate
`910fc54a98e7da644bc5e170281fd935e429692f`.
Every package completed with `ok`, including `cmd/made`, `internal/agent`,
`internal/api`, `internal/config`, `internal/daemon`, `internal/evidence`,
`internal/github`, `internal/orchestrator`, and every pipeline package.

## Vet and lint

Commands:

```text
env SSH_AUTH_SOCK= GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_SYSTEM=/dev/null go vet ./...
env SSH_AUTH_SOCK= GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_SYSTEM=/dev/null golangci-lint run ./...
```

Results:

```text
go vet ./...: exit code 0, no output
golangci-lint run ./...: 0 issues.
```

## Scope and diagnostics

Command:

```text
git diff --check
git rev-parse HEAD
git rev-parse 3e19ed9d598a68149da5a73949533e8095ca4403
```

Results:

```text
git diff --check: exit code 0
HEAD before this documentation refresh: 910fc54a98e7da644bc5e170281fd935e429692f
base: 3e19ed9d598a68149da5a73949533e8095ca4403
```

LSP diagnostics were run for all 50 changed Go files from the exact base.
No errors, warnings, information diagnostics, or hints remained.

The real Made binary manual-QA receipt for the same exact source is in
`evidence/phase-4-manual-qa.md`.

The initial isolated-suite rebase failure was reproduced, explained as missing
child Git identity under signing isolation, fixed in Made, and re-run GREEN in
`evidence/phase-3-lifecycle-durability.md`.

This evidence refresh is documentation-only and is committed after the source
and test validation candidate; it does not change Made source or tests.

## Final conflict-repair and durability correction

The conflict-repair merge is `0a7c21d6d3001b85b38330766e01980bd5e92f2c`.

The final source SHA is `918da271aa9521d292bbda22a862591b770f9af6`.

The compaction regression command `env SSH_AUTH_SOCK= GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_SYSTEM=/dev/null GIT_CONFIG_NOSYSTEM=1 GIT_TERMINAL_PROMPT=0 go test ./internal/daemon -run '^TestRunManager_CompactionPersistsTriggeringTransition$' -count=1` exited `0`.

The affected daemon package command `env SSH_AUTH_SOCK= GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_SYSTEM=/dev/null GIT_CONFIG_NOSYSTEM=1 GIT_TERMINAL_PROMPT=0 go test ./internal/daemon -count=1` exited `0`.

The final ordinary suite command `env SSH_AUTH_SOCK= GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_SYSTEM=/dev/null GIT_CONFIG_NOSYSTEM=1 GIT_TERMINAL_PROMPT=0 go test ./... -count=1` exited `0`.

The final race and shuffle command `env SSH_AUTH_SOCK= GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_SYSTEM=/dev/null GIT_CONFIG_NOSYSTEM=1 GIT_TERMINAL_PROMPT=0 go test -race -shuffle=on -count=1 ./...` exited `0` for every package.

The final build command `env SSH_AUTH_SOCK= GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_SYSTEM=/dev/null GIT_CONFIG_NOSYSTEM=1 GIT_TERMINAL_PROMPT=0 go build ./...` exited `0`.

The final vet command `env SSH_AUTH_SOCK= GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_SYSTEM=/dev/null GIT_CONFIG_NOSYSTEM=1 GIT_TERMINAL_PROMPT=0 go vet ./...` exited `0`.

The configured lint command `env SSH_AUTH_SOCK= GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_SYSTEM=/dev/null GIT_CONFIG_NOSYSTEM=1 GIT_TERMINAL_PROMPT=0 make lint` printed `0 issues.` and exited `0`.

The formatting command `test -z "$(gofmt -l internal cmd)"` exited `0`.

## Conflict-repair validation at final HEAD

The conflict-repair merge is `0a7c21d6d3001b85b38330766e01980bd5e92f2c`.

The final source fix commit is `bac8ed2777f584d98eb1ba8015cf1269d01a8c1e`.

The exact requested base remains `3e19ed9d598a68149da5a73949533e8095ca4403`.

The ordinary full suite command `env SSH_AUTH_SOCK= GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_SYSTEM=/dev/null GIT_CONFIG_NOSYSTEM=1 GIT_TERMINAL_PROMPT=0 go test ./... -count=1` exited `0`.

The final race and shuffle command `env SSH_AUTH_SOCK= GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_SYSTEM=/dev/null GIT_CONFIG_NOSYSTEM=1 GIT_TERMINAL_PROMPT=0 go test -race -shuffle=on -count=1 ./...` exited `0` for every package.

The final build command `env SSH_AUTH_SOCK= GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_SYSTEM=/dev/null GIT_CONFIG_NOSYSTEM=1 GIT_TERMINAL_PROMPT=0 go build ./...` exited `0`.

The final vet command `env SSH_AUTH_SOCK= GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_SYSTEM=/dev/null GIT_CONFIG_NOSYSTEM=1 GIT_TERMINAL_PROMPT=0 go vet ./...` exited `0`.

The configured lint command `env SSH_AUTH_SOCK= GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_SYSTEM=/dev/null GIT_CONFIG_NOSYSTEM=1 GIT_TERMINAL_PROMPT=0 make lint` printed `golangci-lint run ./...` and `0 issues.` and exited `0`.

The formatting command `test -z "$(gofmt -l internal cmd)"` exited `0`.

The exact final worktree SHA at this receipt is `bac8ed2777f584d98eb1ba8015cf1269d01a8c1e`.

The separate review suggestion to invoke `make lint all` is not the repository-configured lint command and is not a required brief command; the configured `make lint` target passed as recorded above.
