# Made remediation phases 1-3 delivery report

This report records the Made-owned remediation delivery from custody base `3e19ed9d598a68149da5a73949533e8095ca4403` through the direct-PR handoff.

The work was performed only in `/Users/douglasjarquin/.herdr/worktrees/made/cs-made-remediation-p1p3b` on branch `cs/made-remediation-p1p3b`.

The failed-launch custody branch remained untouched and the shared Made daemon was never restarted or stopped.

## Isolation and baseline

The isolation commands were `pwd -P`, `git rev-parse --show-toplevel`, `git branch --show-current`, and `git rev-parse HEAD`.

They resolved to the disposable Herdr worktree, branch `cs/made-remediation-p1p3b`, and base SHA `3e19ed9d598a68149da5a73949533e8095ca4403`.

The baseline toolchain was Go `1.26.6 darwin/arm64` and golangci-lint `2.11.2`.

The committed module and CI pin Go `1.26.5`; the local Go `1.26.6` toolchain was used only for this validation run.

The baseline normal, race, vet, and lint commands passed after applying the process-local signing override `GIT_CONFIG_COUNT=1 GIT_CONFIG_KEY_0=commit.gpgsign GIT_CONFIG_VALUE_0=false`.

The signing override was needed because the inherited global SSH signing configuration requires an unavailable 1Password socket.

The installed Codex CLI was `/opt/homebrew/bin/codex` version `codex-cli 0.147.0`.

The supported invocation is `codex exec --cd <directory> --json --output-schema <schema> -`.

## Phase 1 RED contract

The red command was `GIT_CONFIG_COUNT=1 GIT_CONFIG_KEY_0=commit.gpgsign GIT_CONFIG_VALUE_0=false GOTOOLCHAIN=local go test -count=1 ./...`.

The command exited `1` before production changes and its output was captured in `/tmp/made-remediation-p1p3b-red.log`.

The red failures covered unknown versioned commands, global-latest status fallback, missing schema fields, unknown decisions, restart loss, missing doctor JSON, obsolete Codex invocation, destructive socket cleanup, unversioned and zero-value configuration, predecessor lifecycle values, restart-unsafe IDs, incorrect idle handling for `awaiting_merge`, stale PID shutdown, evidence traversal and size bounds, PR URLs used where workflow run IDs are required, authentication classified as a failed check, duplicate PR creation, incorrect rebase conflict classification, and dirty auto-fixes.

Each failure exercised a production boundary with a strict assertion on the required observable behavior, so the failure was a missing implementation contract rather than a permissive fixture mismatch.

The later config replacement RED regression exited at compile time because the opened-descriptor reader did not yet exist; `/tmp/made-remediation-p1p3b-config-boundary-red.log` records that missing production behavior before the fix.

The exact-cap durable RED regression failed on replay because the newline delimiter was counted against the payload cap; `/tmp/made-remediation-p1p3b-durable-cap-red.log` records that missing recovery behavior before the fix.

The compatibility fake GitHub boundary rejected PR URLs where workflow run IDs were required and modeled check status, conclusion, workflow run ID, and details URL.

The fake Codex boundary accepted only the installed `codex exec --cd --json --output-schema` invocation and strict structured output.

The old mocks were updated or removed so they do not authorize commands that the real Made binary does not implement.

## Phase 2 implementation and GREEN

The initial implementation commit was `f92baaf345dea88a907e29e8727aa6d937902df9` with subject `feat: deliver versioned durable remediation contract`.

The durability follow-up commit was `deea4ff0a37c7ac2118a2125a487316b65162d8b` with subject `fix: close remediation durability review gaps`.

The review-boundary follow-up commit was `3b387b78c9b3a6ec393d29fc866be0ff138a871b` with subject `fix: harden remediation review boundaries`.

The final validation-fix commit is `d866545b1391e25f738930200566ab7dcff5c4e5` with subject `fix: close final validation findings`.

The final boundary-hardening commit is `1f8055eeab3fb93b34bf764911f0aec7bfb54767` with subject `fix: close remediation boundary review gaps`.

The evidence-only QA commit is `af5010d7bd910bfa829e030c0198cae909188e69` with subject `docs: record final remediation QA evidence`.

The final executable boundary-fix commit is `d45f5c518664db5f73f42d1d4db595216331f24b` with subject `fix: close final remediation boundary gaps`.

The final boundary-completion commit is `da8f5653bc3e13877480728bc3dd2daf296e7dd2` with subject `fix: harden final remediation boundaries`.

The final input-boundary commit is `8d196c4af539c6cae53fb308c029fb7c700b992f` with subject `fix: bound durable and socket inputs`.

The final config descriptor-boundary commit is `6cab7c9603dc8f0d1fce1c7b114282867ff64c95` with subject `fix: close config read race`.

The final durable replay-boundary commit is `5cf18b3d491f4f244f9c586907ab70509b978317` with subject `fix: replay exact-cap durable records`.

Those follow-ups harden Made home ownership and permissions, private evidence permissions, version-only configuration rejection, disabled-stage representation, environment-injected real Consigliere compatibility testing, final review/API boundaries, bounded configuration and socket input, torn-tail WAL recovery, replacement-safe config reads, and exact-cap durable replay.

The implementation acquires the singleton before socket preparation, uses `lstat`, removes only a stale owner-owned Unix socket, rejects regular files, symlinks, and directories, preserves duplicate owners, and authorizes shutdown through the owner-only socket.

The daemon persists complete run snapshots in an fsync-backed append-only WAL and persists idempotent gate submissions in an fsync-backed spool keyed by gate, ref, and SHA.

Run snapshots remain retained in the local WAL until explicit operator archival outside this task, while evidence is bounded to 1 MiB per file and 4 MiB per run.

Submission admission is closed under the same mutex as the shutdown check, so a concurrent run or gate submission cannot arrive after a successful shutdown decision.

The public surface is versioned and structured through `made capabilities --json`, `made run submit`, `made run status`, `made run list`, `made run cancel`, `made review decide`, and `made doctor --json`.

The lifecycle states are `queued`, `running`, `awaiting_review`, `awaiting_merge`, `succeeded`, `failed`, `canceled`, and `superseded`.

Execution completion is represented separately by `execution_finished`.

Cancellation requires an exact run ID, is idempotent for an already canceled run, waits for cooperative execution to finish at the CLI boundary, and refuses unknown or unrelated runs.

Restored queued, running, and awaiting-review snapshots are reconciled to durable failed state after a daemon restart because no worker can safely resume execution without a durable work specification.

Pending gate submissions are replayed on daemon startup and remain undrained when their external boundary is unavailable.

The orchestrator records stages, disabled stages as `skipped`, findings, decisions, PR URL, errors, supersession, cancellation, and submission events.

The direct `run.submit` API requires a Made-owned gate path, branch ref, and immutable input head, then executes the same real gate pipeline as `gate.notify-push`.

Gate submissions are fsync-enqueued before default-branch refresh, so a reachable daemon cannot lose an accepted update when the external remote is unavailable.

Gate RPCs validate the Made-owned gate layout, bare-repository identity, and exact pushed ref head before creating a run.

The run manager persists the actual prepared output SHA before pushing, while the in-repository evidence mode commits bounded redacted evidence into the pushed branch for later access.

## Phase 3 implementation and GREEN

The `.made.yml` boundary is versioned, strictly decoded, and rejects unknown or zero-value configuration.

The pipeline refreshes the real remote default branch before trusted policy or rebase decisions and fails closed for unavailable remotes, while treating an absent default ref as an empty trusted-policy case.

The review adapter validates a Made-owned schema and uses the installed structured Codex invocation.

Auto-fixes require a clean state, require explicitly returned tracked paths, reject forbidden or untracked paths and forbidden patch headers before apply, record pre-fix and post-fix SHAs, and rerun relevant validation.

Rebase failures are classified as conflicts only when unmerged paths exist.

Evidence is run- and stage-specific, bounded, redacted for common credential assignments and URLs, symlink-safe, and published only through accessible paths.

In-repository evidence is committed into the pushed branch before the push stage completes, while orphan evidence remains on its dedicated evidence branch.

When a remote default ref disappears, Made deletes the cached trusted ref before resolving policy, preventing stale trusted configuration from surviving refresh.

Trusted review and CI required settings control whether disabled review and CI stages block delivery, and `no_ci` explicitly disables the CI requirement.

Pull request creation is idempotent by repository, base, and head.

CI polling uses actual check status, conclusion, workflow run ID, and details URL, while authentication and API failures are infrastructure failures.

Pull-request GitHub authentication and API failures now remain infrastructure errors instead of being represented as failed checks.

The Made CI workflow validates the pinned Go version with race, vet, and pinned lint jobs.

## Validation evidence

The final executable source SHA covered by this validation section is `5cf18b3d491f4f244f9c586907ab70509b978317`.

The config descriptor-boundary commit adds replacement-safe reads from one opened descriptor and a regression proving a replaced path cannot bypass the byte cap.

The durable replay-boundary commit permits the newline delimiter for exact-cap payloads and adds WAL and spool reopen regressions for the exact append limit.

The validation shell exported `GIT_CONFIG_GLOBAL=/dev/null`, `GIT_CONFIG_SYSTEM=/dev/null`, `GOTOOLCHAIN=local`, and deterministic Git author and committer identities for the fixture rebase commits.

It ran `gofmt -l internal cmd`, `go build ./...`, `go test -count=1 -timeout=10m ./...`, `go test -race -shuffle=on -count=1 -timeout=10m ./...`, `go vet ./...`, and `golangci-lint run --timeout=5m --max-issues-per-linter=0 --max-same-issues=0 ./...`.

All final validation commands exited `0`, and golangci-lint reported `0 issues`.

The full validation transcript is `/tmp/made-remediation-p1p3b-5cf18b3-validation.log`.

The fresh real-process manual QA transcript for the final executable SHA is `/tmp/made-remediation-p1p3b-manual-5cf18b3.log`, and its final marker was `manual-qa-5cf18b3=PASS` at that full SHA.

That exact-SHA scenario used a fresh final binary and task-local Made homes to observe daemon cancellation through a real process, doctor through the real Consigliere script, oversized socket rejection, duplicate singleton ownership, raw protocol-version rejection, preserved socket ownership, replacement-safe config reads, torn-tail recovery, and exact-cap durable replay.

The preceding full-pipeline manual scenario at `a724f6c857e903ce52d62d803c540b27a221d6f3` observed real gate initialization and hook execution, native `run.submit` pipeline execution, durable offline gate spooling and replay, exact submission and SHA preservation, exact status and active-list queries, versioned review decision output, WAL restart, duplicate singleton start, and predecessor command rejection.

The `8d196c4` changes are limited to input bounds and durable tail recovery, the `6cab7c9` change is limited to replacement-safe config reads, and the `5cf18b3` change is limited to exact-cap durable replay, while the exact-SHA focused scenario covers all changed runtime surfaces and preserves the full-pipeline evidence from the unchanged executable ancestor.

The compatibility subscenario used the real `bin/cs-made-lib.sh` script, the real Made binary, a strict fake `gh auth status` boundary, a task-local unavailable Herdr socket, and accepted the expected nonzero health exit only after asserting valid versioned JSON and authenticated GitHub state.

The first manual cancellation run returned `running` before the worker completed, which falsified the CLI response contract.

The cancellation wait fix returned `canceled` with `execution_finished=true` in the counterfactual rerun.

The first full validation exposed a WAL replay ordering race where a stale `succeeded` snapshot could be appended after a newer `awaiting_merge` snapshot.

Serializing snapshot capture with WAL append removed that race, and `go test -shuffle=on -count=10 ./internal/daemon -run '^TestPersistentRunStateIncludesSubmissionAndDecisionData$'` passed afterward.

The final changed-file authority is `git diff --name-status 3e19ed9d598a68149da5a73949533e8095ca4403..5cf18b3d491f4f244f9c586907ab70509b978317`, which reports the Made-only paths from the custody base.

At directory level, the base-to-final diff is limited to `.github/workflows/ci.yml`, `AGENTS.md`, `CLAUDE.md`, `README.md`, `cmd/made`, `docs/remediation`, `internal/agent`, `internal/api`, `internal/config`, `internal/daemon`, `internal/evidence`, `internal/exec`, `internal/github`, `internal/orchestrator`, `internal/pipeline`, `internal/skill`, and `skills/made/SKILL.md`.

The final durable-boundary commit-only diff is `git diff --name-status 6cab7c9603dc8f0d1fce1c7b114282867ff64c95..5cf18b3d491f4f244f9c586907ab70509b978317` and contains only daemon implementation and contract-test files.

The separate Made project plan `plans/made-rewrite.md` retains its broader F3 checkbox because that criterion requires running the full Consigliere `--mode made` soldier flow and changing shared Herdr lifecycle state.

This task explicitly forbids running `/made`, editing the Consigliere repository, and stopping or restarting the shared Made daemon, so the Made-specific manual QA above does not claim completion of that broader plan item.

No Consigliere repository file, GitHub issue, default branch, merge, or shared daemon state was changed.

## Delivery dependency

The remaining dependency after this report is the exact-SHA review pass and direct PR on `cs/made-remediation-p1p3b` against `main`.

The branch must be committed, pushed only to `origin/cs/made-remediation-p1p3b`, and opened as a direct PR before the Made lane reports done.
