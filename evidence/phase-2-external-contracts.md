# Phase 2 external-tool contract evidence

Base: `3e19ed9d598a68149da5a73949533e8095ca4403`

## GitHub CLI and CI

The RED contract required the Made adapter to authenticate explicitly, invoke
`gh pr checks` with the supported JSON fields, preserve workflow run IDs from
check links, and reject PR URLs at the run-log and rerun boundary.

Command:

```text
env SSH_AUTH_SOCK= GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_SYSTEM=/dev/null go test ./internal/github -run 'Test(PRChecks|StrictFakeGH|AuthStatus|CreatePR|CheckLogs|RerunCheck)' -count=1
```

Result:

```text
ok   github.com/douglasjarquin/made/internal/github  2.064s
```

The strict fake rejects legacy `gh pr view ... mergeStateStatus`, arbitrary
arguments, PR URLs passed to `gh run view` or `gh run rerun`, and malformed
workflow run IDs.

Command:

```text
env SSH_AUTH_SOCK= GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_SYSTEM=/dev/null go test ./internal/pipeline/ci -run 'TestRun_' -count=1
```

Result:

```text
ok   github.com/douglasjarquin/made/internal/pipeline/ci  3.613s
```

The CI adapter now consumes `name,state,bucket,link`, treats the command exit
status as the check failure boundary, and passes the numeric workflow run ID
to logs and rerun operations.

## Codex structured review adapter

The RED contract required a strict structured invocation, a required findings
array, and explicit rejection of unsupported Claude behavior.

Command:

```text
env SSH_AUTH_SOCK= GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_SYSTEM=/dev/null go test ./internal/agent/... -count=1
```

Result:

```text
ok   github.com/douglasjarquin/made/internal/agent  1.004s
?    github.com/douglasjarquin/made/internal/agent/agenttest  [no test files]
```

Command:

```text
env SSH_AUTH_SOCK= GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_SYSTEM=/dev/null go test ./internal/pipeline/review -run 'TestRun_(AutoFixApplied|AskUserFindingQueued|BlockingFindingHaltsStage)' -count=1
```

Result:

```text
ok   github.com/douglasjarquin/made/internal/pipeline/review  1.278s
```

The adapter invokes only `exec --json --output-schema <schema>
--output-last-message <file> --ephemeral -C <worktree> <task>`, reads the
structured output file, rejects missing or null `findings`, rejects unknown
JSON fields and trailing values, and rejects Claude before process launch.
The fake Codex boundary rejects obsolete or invented argument shapes.

## LSP diagnostics

Command-equivalent diagnostics were run for the changed GitHub client, CI
adapter, strict fake, and focused contract tests.

Result: no errors or warnings were reported.

The initial focused CI diagnostic emitted one non-blocking `stringsseq`
efficiency hint at `internal/pipeline/ci/ci_contract_test.go:56`.
That hint was cleared before the final all-changed-Go-file diagnostic pass.
