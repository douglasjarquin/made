# Made Managed Validation V1

Version: 1  
Protocol version: 1  
Schema version: 1

---

## 1. Purpose

`made validate --managed` is an additive, short-lived, daemonless execution shape
that Consigliere invokes to validate an immutable input commit SHA.

Made validates. Consigliere orchestrates.

---

## 2. Ownership boundary

### Made owns

- Loading and verifying a trusted Made policy snapshot
- Verifying the immutable workspace HEAD equals the supplied input SHA
- Executing validation stages: review, test, document, lint
- Running the configured review Agent in report-only mode
- Producing structured findings with stable fingerprints
- Applying supplied Decisions to matching ask-user findings
- Writing validation evidence outside the Agent workspace
- Emitting a versioned JSON event stream to stdout
- Returning one terminal validation outcome

### Consigliere owns

- Missions, Attempts, workspaces, Agent lifecycle
- Human Questions and boss Decisions
- Repair budgets and repair Attempts
- Retries, scheduling
- Push, pull requests, CI lifecycle, merge authorization, merge
- Notifications

---

## 3. CLI contract

```
made validate --managed --json-events \
  --run-id <opaque-string> \
  --mission-id <opaque-string> \
  --workspace /absolute/path/to/workspace \
  --base-sha <40-hex-sha> \
  --input-sha <40-hex-sha> \
  --trusted-config /absolute/path/to/.made.yml \
  --policy-hash sha256:<64-lowercase-hex> \
  --evidence-dir /absolute/path/to/evidence \
  [--decisions /absolute/path/to/decisions.json]
```

All flags are required except `--decisions`.

### Flag semantics

| Flag | Requirement |
|---|---|
| `--managed` | Required; identifies managed mode |
| `--json-events` | Required; enables JSON-lines stdout protocol |
| `--run-id` | Opaque; echoed in every event |
| `--mission-id` | Opaque; echoed in every event |
| `--workspace` | Absolute canonical path to Git working tree |
| `--base-sha` | Full 40-hex commit SHA; ancestor of input |
| `--input-sha` | Full 40-hex commit SHA; must equal workspace HEAD |
| `--trusted-config` | Absolute path to trusted policy file |
| `--policy-hash` | `sha256:<64-lowercase-hex>` of trusted-config bytes |
| `--evidence-dir` | Absolute path outside workspace for evidence output |
| `--decisions` | Optional; path to Decisions JSON file |

---

## 4. Preflight checks

Before any stage begins, managed mode verifies:

1. `workspace` is an absolute canonical path
2. It is an existing Git working tree
3. `HEAD^{commit}` exactly equals `input_sha`
4. `input_sha` is a full 40-hex commit SHA
5. `base_sha` is a full 40-hex commit SHA
6. Both commits exist locally in the worktree
7. `base_sha` is an ancestor of `input_sha`
8. The worktree has no tracked or non-ignored untracked changes (`git status --porcelain --untracked-files=all` is empty)
9. `trusted-config` is an absolute path to a regular file (not a symlink)
10. The trusted config bytes are read exactly once
11. `SHA-256` of those bytes matches `policy_hash`
12. The verified bytes (not a second read) are parsed as the Made config
13. `evidence-dir` is an absolute path
14. `evidence-dir` is outside the Agent workspace (no prefix relationship)
15. The Decisions file, when supplied, matches run_id, mission_id, input_sha, and policy_hash

A preflight failure emits an `infrastructure_error` or usage-error terminal event and exits with code 1 or 2.

---

## 5. Stages

Managed V1 executes exactly these stages in order:

```
review → test → document → lint
```

Managed V1 never executes: intent, rebase, push, pr, ci, merge.

### Stop-at-first-action rule

Managed V1 stops after the first stage that produces a non-pass outcome.
All findings from that stage are reported before stopping.
Later stages do not run.

### Review (report-only)

- Spawns the configured Codex review Agent
- Requires structured JSON output
- Does NOT apply auto-fix patches
- Does NOT create commits
- Emits all findings as `finding.reported` events
- Applies supplied Decisions to ask-user findings
- Classify: unresolved ask-user → `needs_decision`; rejected ask-user → `failed_terminal`; auto-fixable → `failed_retryable`; blocking → `failed_terminal`

### Test

- Runs the trusted configured test command
- Command non-zero exit → `failed_retryable`
- Spawn / evidence failure → `infrastructure_error`

### Document

- Uses exact `base_sha..input_sha`, not mutable branch names
- Unresolved ask-user → `needs_decision`; rejected → `failed_terminal`; approved → continue

### Lint

- Runs the trusted configured lint command
- Command non-zero exit → `failed_retryable`
- Infrastructure failure → `infrastructure_error`
- Pass → `passed` (if all earlier stages also passed)

---

## 6. Nonmutation guarantee

Managed mode must not modify the workspace.

Before and after every stage, managed mode captures:

```
HEAD=$(git rev-parse HEAD)
STATUS=$(git status --porcelain --untracked-files=all)
```

If either changes, managed mode:

1. Stops immediately
2. Emits an `infrastructure_error` terminal event
3. Preserves all collected evidence
4. Does NOT attempt to reset or conceal the mutation
5. Reports that the caller must quarantine or replace the workspace

---

## 7. Trusted configuration contract

1. The caller supplies `--trusted-config` and `--policy-hash`
2. The file is read exactly once with `os.Open` on a regular file (symlinks rejected)
3. SHA-256 is computed over the exact bytes read
4. Hash is compared against `--policy-hash` (format: `sha256:<64-lowercase-hex>`)
5. The verified bytes (not a second read) are parsed
6. No workspace `.made.yml` is read or merged
7. Repository prose cannot enable commands not authorized by the trusted snapshot
8. The verified policy hash appears in every emitted event

---

## 8. Safe Git execution

All Git invocations used by managed mode:

- Strip all `GIT_*` environment variables
- Strip `SSH_AUTH_SOCK`, `SSH_ASKPASS`, `GIT_SSH_COMMAND`, `GIT_ASKPASS`
- Override `GIT_CONFIG_GLOBAL=/dev/null` and `GIT_CONFIG_SYSTEM=/dev/null`
- Set `GIT_TERMINAL_PROMPT=0`
- Pass `-c core.hooksPath=/dev/null`
- Pass `-c core.fsmonitor=false`
- Use explicit argv (no shell interpolation)
- Perform no network Git operation

---

## 9. JSON event protocol

Managed mode writes JSON Lines to stdout only. Diagnostics go to stderr.

### Event envelope

```json
{
  "schema_version": 1,
  "protocol_version": 1,
  "sequence": 1,
  "run_id": "G-229",
  "mission_id": "M-402",
  "input_sha": "2222222222222222222222222222222222222222",
  "policy_hash": "sha256:aabbcc...",
  "event": "run.started",
  "timestamp": "2026-08-18T21:00:00.000000000Z",
  "payload": {}
}
```

### Protocol rules

- `sequence` begins at 1 and increases by exactly 1
- Timestamps are UTC RFC3339 nanosecond precision
- `run_id`, `mission_id`, `input_sha`, `policy_hash` are constant across all events
- Exactly one terminal event is emitted per invocation
- No event is emitted after the terminal event

### Required event types

| Event | When |
|---|---|
| `run.started` | After preflight succeeds |
| `stage.started` | Before each stage begins |
| `finding.reported` | For each finding discovered by a stage |
| `evidence.created` | After evidence is written for a stage |
| `stage.completed` | After each stage finishes |
| `run.completed` | Terminal; exactly once |

Not implemented in V1: `run.checkpointed`, `run.resumed`, `decision.waiting`

---

## 10. Terminal outcomes

The terminal event `run.completed` carries:

```json
{
  "outcome": "passed",
  "stage": "lint",
  "message": "all managed validation stages passed",
  "findings": [],
  "evidence_refs": []
}
```

### Outcome values

| Outcome | Meaning |
|---|---|
| `passed` | All stages passed; all ask-user findings have approving Decisions |
| `needs_decision` | At least one ask-user finding has no applicable Decision |
| `failed_retryable` | Auto-fixable finding, test failure, or lint failure |
| `failed_terminal` | Blocking finding, rejected Decision, or policy violation |
| `infrastructure_error` | Config hash mismatch, malformed agent output, workspace mutation, evidence failure, etc. |
| `canceled` | Context or process cancellation observed; cleanup complete |

### Exit codes

| Code | Outcome |
|---|---|
| 0 | passed |
| 1 | infrastructure_error |
| 2 | usage / contract error |
| 3 | needs_decision |
| 4 | failed_retryable |
| 5 | failed_terminal |
| 130 | canceled |

The JSON terminal event is authoritative. Exit codes are a process-level summary.

---

## 11. Finding contract

```json
{
  "fingerprint": "sha256:<64-hex>",
  "stage": "review",
  "kind": "ask-user",
  "code": "review.architecture_choice",
  "class": "project-judgment",
  "description": "Human-readable explanation",
  "paths": ["internal/example.go"],
  "symbol": "ExampleFunction",
  "patch": null,
  "evidence_refs": []
}
```

### Finding kinds

| Kind | Classification |
|---|---|
| `auto-fixable` | `failed_retryable`; patch reported but not applied |
| `ask-user` | `needs_decision` (no Decision) or continue (approved Decision) |
| `blocking` | `failed_terminal` |

### Fingerprint construction

Components (in order):

1. `"fpv1"` — fingerprint protocol version prefix
2. stage name
3. finding code (or empty string)
4. finding class (or empty string)
5. finding kind
6. sorted, deduplicated, normalized repository-relative paths (separator normalized to `/`)
7. enclosing symbol (or empty string)
8. normalized description (whitespace-collapsed; absolute workspace prefix stripped)

Each component is separated by `\x00`. The fingerprint is `sha256:<hex>` of the UTF-8 joined string.

Line numbers are not used. The fingerprint is stable across minor description changes that do not alter meaning, but semantic perfection is not guaranteed.

---

## 12. Decision input contract

Optional `--decisions` file:

```json
{
  "schema_version": 1,
  "run_id": "G-229",
  "mission_id": "M-402",
  "input_sha": "2222222222222222222222222222222222222222",
  "policy_hash": "sha256:...",
  "decisions": [
    {
      "decision_id": "D-184",
      "finding_fingerprint": "sha256:...",
      "outcome": "approved",
      "scope": "sha_bound",
      "rationale": "Accepted for this validation input"
    }
  ]
}
```

### Supported decision outcomes

- `approved` — permits ask-user finding to continue
- `rejected` — produces `failed_terminal`

### Binding rules

The Decisions file is rejected when:

- Schema version is unsupported
- `run_id`, `mission_id`, `input_sha`, or `policy_hash` differ from CLI flags
- Duplicate `decision_id` values conflict
- Duplicate fingerprints contain conflicting outcomes
- A Decision references a malformed fingerprint

### Application rules

- Approved Decision permits ask-user finding to continue
- Rejected Decision → `failed_terminal`
- Missing Decision for ask-user → `needs_decision`
- A Decision cannot approve an auto-fixable finding
- A Decision cannot override a blocking finding
- Unused Decisions are reported in evidence

---

## 13. Evidence layout

```
<evidence-dir>/
  <run-id>/
    manifest.json
    review/
      response.txt
      findings.json
    test/
      stdout.log
      stderr.log
    document/
      findings.json
    lint/
      stdout.log
      stderr.log
    terminal.json
```

### terminal.json

Summarizes the complete run:

```json
{
  "run_id": "G-229",
  "mission_id": "M-402",
  "base_sha": "1111111111111111111111111111111111111111",
  "input_sha": "2222222222222222222222222222222222222222",
  "policy_hash": "sha256:...",
  "stage_results": [],
  "findings": [],
  "decisions_applied": [],
  "outcome": "passed",
  "event_count": 12,
  "evidence_refs": [],
  "made_version": "..."
}
```

- No evidence commit is created
- No evidence branch is pushed
- Evidence writes are atomic where practical (write-temp-then-rename)
- An evidence-write failure cannot be reported as validation success

---

## 14. Compatibility guarantees

Managed mode does not modify:

- `made run submit` / `status` / `list` / `cancel`
- `made review decide`
- `made daemon`
- `made gate`
- `made doctor`
- `made capabilities`
- The standalone review auto-fix behavior
- The standalone pipeline's `parkForApproval` wait
- Any daemon persistence

`made capabilities --json` is extended additively: `"validate.managed.v1"` is added to the `commands` list.

---

## 15. Crash and cancellation behavior

On OS signal or context cancellation:

1. Made emits one `run.completed` event with outcome `canceled`
2. Exits with code 130
3. Evidence collected up to cancellation is preserved
4. No cleanup of the workspace is attempted
5. The workspace state after cancellation is undefined; the caller should treat it as potentially dirty

On an unexpected panic: evidence is best-effort. The exit code is non-zero (not 130). The caller should treat the run as `infrastructure_error`.

---

## 16. Explicit non-goals

Managed V1 does not implement:

- Consigliere integration code or client
- Mission repair budgets or Mission-level waiver authorization
- Workspace creation or trusted mirror creation
- Privileged Git push, PR creation, CI monitoring, merge
- Made checkpoint/resume
- Made stage caching
- Bidirectional tracker synchronization
- Herdr integration
- A second Agent kind
- Generic SCM adapters
- A TUI or new persistence database
- A replacement for the standalone daemon

---

## 17. Sample invocations and streams

### Sample invocation

```bash
made validate --managed --json-events \
  --run-id G-229 \
  --mission-id M-402 \
  --workspace /tmp/ws/repo \
  --base-sha 1111111111111111111111111111111111111111 \
  --input-sha 2222222222222222222222222222222222222222 \
  --trusted-config /trusted/.made.yml \
  --policy-hash sha256:aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899 \
  --evidence-dir /evidence \
  --decisions /decisions/G-229.json
```

### Sample passing stream

```jsonl
{"schema_version":1,"protocol_version":1,"sequence":1,"run_id":"G-229","mission_id":"M-402","input_sha":"2222222222222222222222222222222222222222","policy_hash":"sha256:aabb...","event":"run.started","timestamp":"2026-08-18T21:00:00.000000000Z","payload":{}}
{"schema_version":1,"protocol_version":1,"sequence":2,"run_id":"G-229","mission_id":"M-402","input_sha":"2222222222222222222222222222222222222222","policy_hash":"sha256:aabb...","event":"stage.started","timestamp":"2026-08-18T21:00:00.100000000Z","payload":{"stage":"review"}}
{"schema_version":1,"protocol_version":1,"sequence":3,"run_id":"G-229","mission_id":"M-402","input_sha":"2222222222222222222222222222222222222222","policy_hash":"sha256:aabb...","event":"evidence.created","timestamp":"2026-08-18T21:00:05.000000000Z","payload":{"stage":"review","path":"G-229/review/findings.json"}}
{"schema_version":1,"protocol_version":1,"sequence":4,"run_id":"G-229","mission_id":"M-402","input_sha":"2222222222222222222222222222222222222222","policy_hash":"sha256:aabb...","event":"stage.completed","timestamp":"2026-08-18T21:00:05.100000000Z","payload":{"stage":"review","outcome":"passed"}}
{"schema_version":1,"protocol_version":1,"sequence":5,"run_id":"G-229","mission_id":"M-402","input_sha":"2222222222222222222222222222222222222222","policy_hash":"sha256:aabb...","event":"stage.started","timestamp":"2026-08-18T21:00:05.200000000Z","payload":{"stage":"test"}}
{"schema_version":1,"protocol_version":1,"sequence":6,"run_id":"G-229","mission_id":"M-402","input_sha":"2222222222222222222222222222222222222222","policy_hash":"sha256:aabb...","event":"evidence.created","timestamp":"2026-08-18T21:00:10.000000000Z","payload":{"stage":"test","path":"G-229/test/stdout.log"}}
{"schema_version":1,"protocol_version":1,"sequence":7,"run_id":"G-229","mission_id":"M-402","input_sha":"2222222222222222222222222222222222222222","policy_hash":"sha256:aabb...","event":"stage.completed","timestamp":"2026-08-18T21:00:10.100000000Z","payload":{"stage":"test","outcome":"passed"}}
{"schema_version":1,"protocol_version":1,"sequence":8,"run_id":"G-229","mission_id":"M-402","input_sha":"2222222222222222222222222222222222222222","policy_hash":"sha256:aabb...","event":"stage.started","timestamp":"2026-08-18T21:00:10.200000000Z","payload":{"stage":"document"}}
{"schema_version":1,"protocol_version":1,"sequence":9,"run_id":"G-229","mission_id":"M-402","input_sha":"2222222222222222222222222222222222222222","policy_hash":"sha256:aabb...","event":"stage.completed","timestamp":"2026-08-18T21:00:10.300000000Z","payload":{"stage":"document","outcome":"passed"}}
{"schema_version":1,"protocol_version":1,"sequence":10,"run_id":"G-229","mission_id":"M-402","input_sha":"2222222222222222222222222222222222222222","policy_hash":"sha256:aabb...","event":"stage.started","timestamp":"2026-08-18T21:00:10.400000000Z","payload":{"stage":"lint"}}
{"schema_version":1,"protocol_version":1,"sequence":11,"run_id":"G-229","mission_id":"M-402","input_sha":"2222222222222222222222222222222222222222","policy_hash":"sha256:aabb...","event":"evidence.created","timestamp":"2026-08-18T21:00:11.000000000Z","payload":{"stage":"lint","path":"G-229/lint/stdout.log"}}
{"schema_version":1,"protocol_version":1,"sequence":12,"run_id":"G-229","mission_id":"M-402","input_sha":"2222222222222222222222222222222222222222","policy_hash":"sha256:aabb...","event":"stage.completed","timestamp":"2026-08-18T21:00:11.100000000Z","payload":{"stage":"lint","outcome":"passed"}}
{"schema_version":1,"protocol_version":1,"sequence":13,"run_id":"G-229","mission_id":"M-402","input_sha":"2222222222222222222222222222222222222222","policy_hash":"sha256:aabb...","event":"run.completed","timestamp":"2026-08-18T21:00:11.200000000Z","payload":{"outcome":"passed","stage":"lint","message":"all managed validation stages passed","findings":[],"evidence_refs":[]}}
```

### Sample needs-decision stream (review ask-user, no Decision supplied)

```jsonl
{"schema_version":1,"protocol_version":1,"sequence":1,...,"event":"run.started","payload":{}}
{"schema_version":1,"protocol_version":1,"sequence":2,...,"event":"stage.started","payload":{"stage":"review"}}
{"schema_version":1,"protocol_version":1,"sequence":3,...,"event":"finding.reported","payload":{"fingerprint":"sha256:1234...","stage":"review","kind":"ask-user","code":"review.architecture_choice","description":"New dependency added without ADR","paths":["go.mod"]}}
{"schema_version":1,"protocol_version":1,"sequence":4,...,"event":"run.completed","payload":{"outcome":"needs_decision","stage":"review","message":"1 ask-user finding(s) require a Decision","findings":[...],"evidence_refs":[]}}
```

### Sample failed-retryable stream (auto-fixable review finding)

```jsonl
{"schema_version":1,"protocol_version":1,"sequence":1,...,"event":"run.started","payload":{}}
{"schema_version":1,"protocol_version":1,"sequence":2,...,"event":"stage.started","payload":{"stage":"review"}}
{"schema_version":1,"protocol_version":1,"sequence":3,...,"event":"finding.reported","payload":{"fingerprint":"sha256:abcd...","stage":"review","kind":"auto-fixable","code":"review.formatting","description":"gofmt needed","paths":["internal/foo.go"],"patch":"--- a/internal/foo.go\n+++ b/internal/foo.go\n..."}}
{"schema_version":1,"protocol_version":1,"sequence":4,...,"event":"run.completed","payload":{"outcome":"failed_retryable","stage":"review","message":"1 auto-fixable finding(s) require repair","findings":[...],"evidence_refs":[]}}
```
