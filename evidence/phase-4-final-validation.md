# Phase 4 final local validation evidence

The earlier ledger receipt was recorded at
`afea024e1da9f59be9181c18f18b11793a782f36`.
After the final managed-gate, cancellation, and durable-publication
corrections, the source and test validation candidate is
`60420902ea5b1ed434f57c86ebb0e85be7be5281`.
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
`60420902ea5b1ed434f57c86ebb0e85be7be5281`.
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
HEAD before this documentation refresh: 60420902ea5b1ed434f57c86ebb0e85be7be5281
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
