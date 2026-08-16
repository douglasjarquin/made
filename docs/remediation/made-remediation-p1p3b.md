# Made remediation phases 1-3 delivery report

This report records the Made-owned remediation delivery from custody base `3e19ed9d598a68149da5a73949533e8095ca4403` through the direct-PR handoff.

The work was performed only in `/Users/douglasjarquin/.herdr/worktrees/made/cs-made-remediation-p1p3b` on branch `cs/made-remediation-p1p3b`.

The failed-launch custody branch remained untouched and the shared Made daemon was never restarted or stopped.

## Isolation and baseline

The isolation commands were `pwd -P`, `git rev-parse --show-toplevel`, `git branch --show-current`, and `git rev-parse HEAD`.

They resolved to the disposable Herdr worktree, branch `cs/made-remediation-p1p3b`, and base SHA `3e19ed9d598a68149da5a73949533e8095ca4403`.

The baseline toolchain was Go `1.26.6 darwin/arm64` and golangci-lint `2.11.2`.

The baseline normal, race, vet, and lint commands passed after applying the process-local signing override `GIT_CONFIG_COUNT=1 GIT_CONFIG_KEY_0=commit.gpgsign GIT_CONFIG_VALUE_0=false`.

The signing override was needed because the inherited global SSH signing configuration requires an unavailable 1Password socket.

The installed Codex CLI was `/opt/homebrew/bin/codex` version `codex-cli 0.147.0`.

The supported invocation is `codex exec --cd <directory> --json --output-schema <schema> -`.

## Phase 1 RED contract

The red command was `GIT_CONFIG_COUNT=1 GIT_CONFIG_KEY_0=commit.gpgsign GIT_CONFIG_VALUE_0=false GOTOOLCHAIN=local go test -count=1 ./...`.

The command exited `1` before production changes and its output was captured in `/tmp/made-remediation-p1p3b-red.log`.

The red failures covered unknown versioned commands, global-latest status fallback, missing schema fields, unknown decisions, restart loss, missing doctor JSON, obsolete Codex invocation, destructive socket cleanup, unversioned and zero-value configuration, predecessor lifecycle values, restart-unsafe IDs, incorrect idle handling for `awaiting_merge`, stale PID shutdown, evidence traversal and size bounds, PR URLs used where workflow run IDs are required, authentication classified as a failed check, duplicate PR creation, incorrect rebase conflict classification, and dirty auto-fixes.

Each failure exercised a production boundary with a strict assertion on the required observable behavior, so the failure was a missing implementation contract rather than a permissive fixture mismatch.

The compatibility fake GitHub boundary rejected PR URLs where workflow run IDs were required and modeled check status, conclusion, workflow run ID, and details URL.

The fake Codex boundary accepted only the installed `codex exec --cd --json --output-schema` invocation and strict structured output.

The old mocks were updated or removed so they do not authorize commands that the real Made binary does not implement.

## Phase 2 implementation and GREEN

The initial implementation commit was `f92baaf345dea88a907e29e8727aa6d937902df9` with subject `feat: deliver versioned durable remediation contract`.

The implementation acquires the singleton before socket preparation, uses `lstat`, removes only a stale owner-owned Unix socket, rejects regular files, symlinks, and directories, preserves duplicate owners, and authorizes shutdown through the owner-only socket.

The daemon persists complete run snapshots in an fsync-backed append-only WAL and persists idempotent gate submissions in an fsync-backed spool keyed by gate, ref, and SHA.

The public surface is versioned and structured through `made capabilities --json`, `made run submit`, `made run status`, `made run list`, `made run cancel`, `made review decide`, and `made doctor --json`.

The lifecycle states are `queued`, `running`, `awaiting_review`, `awaiting_merge`, `succeeded`, `failed`, `canceled`, and `superseded`.

Execution completion is represented separately by `execution_finished`.

Cancellation requires an exact run ID, is idempotent for an already canceled run, waits for cooperative execution to finish at the CLI boundary, and refuses unknown or unrelated runs.

Restored queued and running snapshots are reconciled to durable failed state after a daemon restart because no worker can safely resume execution without a durable work specification.

Pending gate submissions are replayed on daemon startup and remain undrained when their external boundary is unavailable.

The orchestrator records stages, disabled stages as `skipped`, findings, decisions, PR URL, errors, supersession, cancellation, and submission events.

## Phase 3 implementation and GREEN

The `.made.yml` boundary is versioned, strictly decoded, and rejects unknown or zero-value configuration.

The pipeline refreshes the real remote default branch before trusted policy or rebase decisions.

The review adapter validates a Made-owned schema and uses the installed structured Codex invocation.

Auto-fixes require a clean state, require explicitly returned tracked paths, reject forbidden or untracked paths, record pre-fix and post-fix SHAs, and rerun relevant validation.

Rebase failures are classified as conflicts only when unmerged paths exist.

Evidence is run- and stage-specific, bounded, redacted, symlink-safe, and published only through accessible paths.

Pull request creation is idempotent by repository, base, and head.

CI polling uses actual check status, conclusion, workflow run ID, and details URL, while authentication and API failures are infrastructure failures.

The Made CI workflow validates the pinned Go version with race, vet, and pinned lint jobs.

## Validation evidence

The targeted command `GIT_CONFIG_COUNT=1 GIT_CONFIG_KEY_0=commit.gpgsign GIT_CONFIG_VALUE_0=false GOTOOLCHAIN=local go test ./internal/daemon ./internal/orchestrator ./cmd/made` passed after the durability fixes.

The final validation set is `GIT_CONFIG_COUNT=1 GIT_CONFIG_KEY_0=commit.gpgsign GIT_CONFIG_VALUE_0=false GOTOOLCHAIN=local go test -count=1 ./...`, `GIT_CONFIG_COUNT=1 GIT_CONFIG_KEY_0=commit.gpgsign GIT_CONFIG_VALUE_0=false GOTOOLCHAIN=local go test -race -shuffle=on -count=1 ./...`, `GOTOOLCHAIN=local go vet ./...`, and `GOTOOLCHAIN=local golangci-lint run --timeout=5m`.

The real-process manual QA transcript is `/tmp/made-remediation-p1p3b-manual-final.log` and its final marker was `manual-qa-final=PASS`.

That scenario used a fresh binary and task-local Made homes to observe capabilities, doctor through the real Consigliere script, exact submission and SHA preservation, exact status and active-list queries, review decision, cancellation, shutdown refusal, WAL restart, duplicate singleton start, stale PID handling, regular-file, symlink, and directory socket rejection, and predecessor command rejection.

The first manual cancellation run returned `running` before the worker completed, which falsified the CLI response contract.

The cancellation wait fix returned `canceled` with `execution_finished=true` in the counterfactual rerun.

The first full validation exposed a WAL replay ordering race where a stale `succeeded` snapshot could be appended after a newer `awaiting_merge` snapshot.

Serializing snapshot capture with WAL append removed that race, and `go test -shuffle=on -count=10 ./internal/daemon -run '^TestPersistentRunStateIncludesSubmissionAndDecisionData$'` passed afterward.

The final changed-file authority is `git diff --name-status 3e19ed9d598a68149da5a73949533e8095ca4403`.

The implementation and contract-test paths in that diff are `.github/workflows/ci.yml`, `AGENTS.md`, `CLAUDE.md`, `README.md`, `cmd/made`, `internal/agent`, `internal/api`, `internal/config`, `internal/daemon`, `internal/evidence`, `internal/exec`, `internal/github`, `internal/orchestrator`, `internal/pipeline`, `internal/skill`, and `skills/made/SKILL.md`.

No Consigliere repository file, GitHub issue, default branch, merge, or shared daemon state was changed.

## Delivery dependency

The remaining dependency after this report is the direct PR on `cs/made-remediation-p1p3b` against `main`.

The branch must be committed, pushed only to `origin/cs/made-remediation-p1p3b`, and opened as a direct PR before the Made lane reports done.
