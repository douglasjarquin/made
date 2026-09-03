# Made Managed Validation V1

Version: 1  
Protocol version: 1  
Schema version: 1

---

## 1. Purpose

`made validate --managed` is an additive, short-lived, daemonless execution shape
that a caller or orchestrator (for example Consigliere, Cursor Cloud, or a CI
job) invokes to validate an immutable input commit SHA.

Made validates. The caller orchestrates.

---

## 2. Ownership boundary

### Made owns

- Loading and verifying a trusted Made policy snapshot
- Deriving the stage plan from that policy: which of review/test/document/lint run, and which are `not_configured` or `disabled`
- Verifying the immutable workspace HEAD equals the supplied input SHA
- Executing every planned stage
- Obtaining Review either by launching Made's configured internal agent (report-only) or by accepting one caller-supplied external review result
- Producing structured findings with stable fingerprints through one shared classification path, regardless of Review source
- Applying supplied Decisions to matching ask-user findings
- Writing validation evidence outside the Agent workspace
- Emitting a versioned JSON event stream to stdout
- Returning one terminal validation outcome

### The caller owns

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
  [--review-source internal|external] \
  [--review-result /absolute/path/to/review-result.json] \
  [--decisions /absolute/path/to/decisions.json]
```

All flags are required except `--review-source`, `--review-result`, and `--decisions`.

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
| `--review-source` | Optional; `internal` (default) or `external` - how Review is obtained when policy enables it |
| `--review-result` | Optional; absolute path to a caller-supplied external review result; required when `--review-source=external` |
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

Managed V1 considers exactly these stages, in order:

```
review → test → document → lint
```

Managed V1 never executes: intent, rebase, push, pr, ci, merge.

### Stage plan

Before any stage runs, Made derives a plan from trusted policy alone. Each
stage resolves to one of:

| State | Meaning |
|---|---|
| `run` | The stage executes |
| `not_configured` | No applicable command/rule/agent is configured; the stage never executes and is never reported as passed |
| `disabled` | `stages.<name>.enabled: false` explicitly turns the stage off; it never executes |

- **Review** runs when `review.required` is `true` or an `agent` is configured; otherwise `not_configured`.
- **Test** runs when `commands.test` or a selected validation-lane `full` command (issue #33) is configured; otherwise `not_configured`.
- **Document** runs when at least one `document.rules` entry is configured; otherwise `not_configured`.
- **Lint** runs when `commands.lint` is configured; otherwise `not_configured`.

Every stage still emits `stage.started`/`stage.completed` even when
`not_configured` or `disabled`, so the terminal result always identifies
exactly which stages ran and which were absent or disabled - never silently.
A run whose plan has no stage in the `run` state fails with
`infrastructure_error` rather than issuing a meaningless `passed` receipt.

### Stop-at-first-action rule

Managed V1 stops after the first `run` stage that produces a non-pass
outcome. All findings from that stage are reported before stopping. Later
stages do not run, but remain visible in `stage_results` at `pending` rather
than being omitted. A `not_configured` or `disabled` stage never stops the
run.

### Review (report-only)

- Obtains a Review via one of two sources, selected by `--review-source` (default `internal`):
  - `internal`: spawns the configured agent in report-only mode; requires structured JSON output
  - `external`: accepts one caller-supplied review result bound to the exact base SHA, input SHA, policy hash, and review-contract hash (section 11a); Made launches no reviewer of its own on this path
- Optionally consults trusted `review.guides` (section 11b); a project with none configured is unaffected
- Does NOT apply auto-fix patches
- Does NOT create commits
- Both sources' findings are normalized through one shared fingerprint/classification/Decision path
- Emits all findings as `finding.reported` events
- Applies supplied Decisions to ask-user findings
- Classify: unresolved ask-user → `needs_decision`; rejected ask-user → `failed_terminal`; auto-fixable → `failed_retryable`; blocking → `failed_terminal`

### Test

- Runs the trusted configured test command plus any validation-lane `full` commands selected for the changed paths (issue #33)
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
  "invocation_id": "1234567890abcdef",
  "base_sha": "1111111111111111111111111111111111111111",
  "input_sha": "2222222222222222222222222222222222222222",
  "policy_hash": "sha256:64aec94d8e1fade3975101ba87f44076e4487016c87c6cf8d24857aad2e28d27",
  "event": "run.started",
  "timestamp": "2026-08-18T21:00:00.000000000Z",
  "payload": {}
}
```

### Protocol rules

- `sequence` begins at 1 and increases by exactly 1
- `invocation_id` is a unique lowercase hex string, constant within a single invocation but different on each rerun
- `base_sha` is the immutable base commit SHA (40-hex), used for diff ranges
- `input_sha` is the immutable input commit SHA (40-hex), equal to workspace HEAD
- Timestamps are UTC RFC3339 nanosecond precision
- `run_id`, `mission_id`, `base_sha`, `input_sha`, `policy_hash`, and `invocation_id` are constant across all events
- Exactly one terminal event is emitted per invocation
- No event is emitted after the terminal event

### Required event types

| Event | When |
|---|---|
| `run.started` | At process start, before preflight validation begins |
| `stage.started` | Before each stage begins (review, test, document, lint) |
| `finding.reported` | For each finding discovered by a stage |
| `evidence.created` | After evidence is written for a stage |
| `stage.completed` | After each stage finishes (even on failure) |
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
| `passed` | Every `run` stage passed; all ask-user findings have approving Decisions |
| `needs_decision` | At least one ask-user finding has no applicable Decision |
| `failed_retryable` | Auto-fixable finding, test failure, or lint failure |
| `failed_terminal` | Blocking finding, rejected Decision, or policy violation |
| `infrastructure_error` | Config hash mismatch, malformed agent/external-review output, workspace mutation, evidence failure, no effective validation work configured, etc. |
| `canceled` | Context or process cancellation observed; cleanup complete |

`not_configured` and `disabled` are stage-level coverage states reported on
`stage.completed` events (section 9); they never appear as a run's terminal
outcome. `pending` is a stage-level coverage state reported only in
`stage_results` (terminal.json, section 13): a stage planned to `run` that
Run stopped before reaching. It is never emitted as a `stage.completed`
event and never appears as a run's terminal outcome.

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

**For managed validation**, fingerprints use structural identity only:

Components (in order):

1. `"fpv1"` — fingerprint protocol version prefix
2. stage name
3. finding code (required; stable rule/defect identifier)
4. finding class (required; stable category)
5. finding kind
6. sorted, deduplicated, normalized repository-relative paths (required; separator normalized to `/`)
7. finding symbol/locus (strongly recommended when applicable; e.g., function name)

Each component is separated by `\x00`. The fingerprint is `sha256:<hex>` of the UTF-8 joined string.

**Important**: The description is intentionally omitted from managed fingerprints to ensure stability
across paraphrasing. This requires all managed findings to provide stable structural fields (code, class, paths).
A finding missing any required structural field is rejected at preflight with `infrastructure_error`.

### Finding identity requirements

For managed validation, every finding must include:

- **code**: Stable, rule- or defect-specific identifier (e.g., `review.security_issue`, `style.naming`)
- **class**: Stable category (e.g., `security`, `style`, `architecture`)
- **paths**: One or more repository-relative paths affected by the finding
- **symbol**: Strongly recommended when applicable (e.g., function name, class name, line range)
- **description**: Human-readable explanation (not used in fingerprint; serves as evidence)

---

## 11a. Review contract and external review-result contract

Made builds one canonical, versioned `ReviewContract` for every run, covering
the finding taxonomy, finding kinds, exact `base_sha`/`input_sha`, the exact
diff instructions, nonmutation/authority rules, and - when `review.guides` is
configured (section 11b) - the resolved guide bindings and read
instructions. Its `sha256:<hex>` hash (`review_contract_hash`) is
reproducible by any caller from the documented schema and these inputs
alone - Made does not need to be asked for it first. The hash changes
whenever guide order, path, or content changes, so a result produced
against stale guides is never silently reused.

A caller choosing `--review-source external` renders this contract for its
own reviewer, then submits one JSON file at `--review-result` matching:

```json
{
  "schema_version": 1,
  "review_contract_version": 1,
  "executor": "cursor",
  "reviewer": "made-reviewer",
  "requested_model": "claude-opus-5[effort=high]",
  "actual_model": null,
  "base_sha": "1111111111111111111111111111111111111111",
  "input_sha": "2222222222222222222222222222222222222222",
  "policy_hash": "sha256:...",
  "review_contract_hash": "sha256:...",
  "findings": []
}
```

Made rejects the result unless: the schema and contract versions are
supported; `base_sha`, `input_sha`, `policy_hash`, and `review_contract_hash`
match exactly; every string and array is within bounds (see
`internal/managed/externalreview.go` for exact limits); every finding has a
valid kind and the same structural identity fields internal findings
require (section 11); and no two findings share a fingerprint with
conflicting kinds. Unknown top-level fields are rejected outright.

`executor`, `reviewer`, `requested_model`, and `actual_model` are
informational provenance only: Made never rejects a result because
`actual_model` is absent, differs from `requested_model`, or equals it. Made
does not select or invoke any model on this path - it only classifies the
findings the caller's reviewer already produced.

---

## 11b. Review guides (issue #40)

A project may point Review at zero or more trusted, repository-relative
guide files under `review.guides` in its config:

```yaml
review:
  guides:
    - ".made/features/README.md"
    - "docs/architecture.md"
```

No guides configured, or an empty list, leaves Review exactly as before.
Guides are generic: a feature map is a recommended convention
(`.made/features/README.md` as a concise index linking to focused files),
not a dedicated Made subsystem - Made does not parse feature IDs, enforce
Markdown headings, or build a behavior graph. Guide paths are always taken
from trusted configuration, never a candidate's own command overrides,
mirroring `test.evidence`.

For each run, Made resolves every configured guide from the trusted root -
the directory implied by `--trusted-config`'s own layout, the only trusted
base managed mode has today (see the note on `made verify`, issue #41,
below) - rejecting an absolute path, a `..` escape, a symlink, a directory,
or anything but a regular file, and enforcing conservative bounds on guide
count, path length, individual and aggregate guide bytes. A configured guide
that is missing or unsafe in the trusted base is an `infrastructure_error`
raised before Review runs, not a Review finding. Candidate edits, deletions,
or additions at the same path, and candidate edits to `review.guides`
itself, never change which bytes govern the current run.

Each resolved guide is bound into the `ReviewContract` as
`{path, content_hash, bytes}`, alongside a fixed instruction:

> Read every configured top-level guide. When a guide is an index, follow
> only the entries relevant to the exact base-to-input change unless the
> change is broad enough to require a full sweep.

Made never eagerly concatenates guide content into the review prompt. The
internal reviewer receives each guide's exact path/hash/bytes plus a bounded
`git show <base_sha>:<guide_path>` read command; an external reviewer
receives the identical metadata through the rendered contract. Guide prose
is context only - Made never executes a command because a guide's text
suggests it, and Test/Lint commands still come only from trusted
configuration.

An external review result may optionally echo a bounded `guides_consulted`
diagnostic:

```json
{
  "guides_consulted": [
    {"path": ".made/features/README.md", "content_hash": "sha256:..."}
  ]
}
```

Each entry must name a currently configured guide with that guide's exact
current `content_hash`; an invented path, a stale hash, a duplicate path, or
more entries than the configured guide count is rejected. `guides_consulted`
is diagnostic, not proof: Made validates paths and hashes but never claims
cryptographic proof that a model read or understood the prose.

Managed mode's trusted base today is exactly the `--trusted-config` file
Made was given, hash-verified against `--policy-hash`; there is no
daemon-mediated "trusted base at a git ref" yet. `made verify` (issue #41)
is expected to add that; until then, guides are resolved from the same
filesystem location the trusted config was read from, not from a git ref.

---

## 12. Decision input contract

Optional `--decisions` file:

```json
{
  "schema_version": 1,
  "run_id": "G-229",
  "mission_id": "M-402",
  "base_sha": "1111111111111111111111111111111111111111",
  "input_sha": "2222222222222222222222222222222222222222",
  "policy_hash": "sha256:64aec94d8e1fade3975101ba87f44076e4487016c87c6cf8d24857aad2e28d27",
  "decisions": [
    {
      "decision_id": "D-184",
      "finding_fingerprint": "sha256:aaabbbcccddd...",
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
- `run_id`, `mission_id`, `base_sha`, `input_sha`, or `policy_hash` differ from CLI flags
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
  <hashed-run-id>/                 (SHA-256 of run_id, lowercase hex, 64 chars)
    <invocation-id>/               (unique per invocation; lowercase hex, 16 chars)
      review/
        findings.json              (structured findings from review Agent)
      test/
        stdout.log                 (test stage output)
        stderr.log                 (test stage errors)
      document/
        findings.json              (documentation findings)
      lint/
        stdout.log                 (lint stage output)
        stderr.log                 (lint stage errors)
      terminal.json                (run summary and terminal outcome)
```

### Referencing evidence

Evidence paths in events are relative to `<evidence-dir>` and include both the hashed run ID and invocation ID:

```
<hashed-run-id>/<invocation-id>/stage/file
```

Example: `64aec94d8e1fade.../1234567890abcdef/review/findings.json`

To resolve an evidence reference, use: `<evidence-dir>/<path-from-event>`

The hashed run ID (`sha256:<run_id>.hex()`) allows multiple invocations (reruns) to share the same hashed
directory while isolating evidence by invocation instance. This enables efficient batch review of reruns
without requiring separate run ID directories.

### terminal.json

Summarizes the complete run:

```json
{
  "schema_version": 2,
  "run_id": "G-229",
  "mission_id": "M-402",
  "base_sha": "1111111111111111111111111111111111111111",
  "input_sha": "2222222222222222222222222222222222222222",
  "policy_hash": "sha256:...",
  "stage_results": [
    {"stage": "review", "outcome": "failed_terminal", "message": "blocking finding: ..."},
    {"stage": "test", "outcome": "pending"},
    {"stage": "document", "outcome": "not_configured", "message": "no document.rules configured"},
    {"stage": "lint", "outcome": "pending"}
  ],
  "findings": [],
  "decisions_applied": [],
  "outcome": "failed_terminal",
  "event_count": 12,
  "evidence_refs": [],
  "made_version": "..."
}
```

`schema_version` versions `terminal.json`'s own shape (currently 2), independent
of the JSON-Lines event envelope's `schema_version` (section 9, currently 1).
It was bumped from an unversioned baseline when `stage_results` started
reporting unreached `run` stages at `pending` instead of omitting them.

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

`made capabilities --json` is extended additively: `"validate.managed.v1"` is added to the `commands` list, and a `managed_validation` object reports `review_sources` (`["internal", "external"]`) and `optional_stages` (`["review", "test", "document", "lint"]`). `"verify"` and `"cursor"` are likewise added to `commands` (project issues #41/#42), advertising the `made verify` and `made cursor` command surfaces; neither bumps `schema_version`.

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

- Any specific caller's integration code or client (Consigliere, Cursor Cloud, or otherwise)
- Mission repair budgets or Mission-level waiver authorization
- Workspace creation or trusted mirror creation
- Privileged Git push, PR creation, CI monitoring, merge
- Made checkpoint/resume
- Made stage caching
- Bidirectional tracker synchronization
- Herdr integration
- A second Agent kind
- Generating Cursor skills or subagent files, or selecting/invoking a Cursor model
- Enforcing which model an external reviewer actually used
- The simpler `made verify` interface (a later issue) or repo-root config discovery for managed mode
- Resolving `review.guides` from a trusted base at a git ref (that arrives with `made verify`, issue #41); V1 resolves guides from the trusted config's own filesystem location
- A dedicated feature-map parser, manifest, initializer, or `feature-map init/check` subsystem
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
