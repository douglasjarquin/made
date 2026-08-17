# Phase 4 final local validation evidence

Validation candidate before this evidence commit:
`afea024e1da9f59be9181c18f18b11793a782f36`.
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

Result: exit code 0.
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
git status --short
git rev-parse HEAD
git rev-parse 3e19ed9d598a68149da5a73949533e8095ca4403
```

Results:

```text
git diff --check: exit code 0
git status --short: clean before this evidence file was added
HEAD: afea024e1da9f59be9181c18f18b11793a782f36
base: 3e19ed9d598a68149da5a73949533e8095ca4403
```

LSP diagnostics were run for every changed Go file from the exact base.
No errors, warnings, information diagnostics, or hints remained.

The initial isolated-suite rebase failure was reproduced, explained as missing
child Git identity under signing isolation, fixed in Made, and re-run GREEN in
`evidence/phase-3-lifecycle-durability.md`.
