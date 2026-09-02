# Made Managed Validation — Integration Reference

This document is written for Consigliere implementers. It describes the exact
Made interface without requiring knowledge of Made internals.

---

## Command

```
made validate --managed --json-events \
  --run-id <opaque> \
  --mission-id <opaque> \
  --workspace /absolute/path/to/workspace \
  --base-sha <40-hex> \
  --input-sha <40-hex> \
  --trusted-config /absolute/path/to/.made.yml \
  --policy-hash sha256:<64-lowercase-hex> \
  --evidence-dir /absolute/path/outside/workspace \
  [--decisions /absolute/path/to/decisions.json]
```

## Required inputs

| Input | Type | Notes |
|---|---|---|
| `run_id` | opaque string | Echoed in every event; never interpreted by Made |
| `mission_id` | opaque string | Echoed in every event; never interpreted by Made |
| `workspace` | absolute path | Must be a Git working tree; HEAD must equal `input_sha` |
| `base_sha` | 40-hex SHA | Ancestor of `input_sha`; used for diff range |
| `input_sha` | 40-hex SHA | Immutable; must exactly equal workspace HEAD |
| `trusted_config` | absolute path | Regular file (not symlink); hash-verified before parsing |
| `policy_hash` | `sha256:<64-hex>` | SHA-256 of `trusted_config` bytes |
| `evidence_dir` | absolute path | Outside `workspace`; created if absent |
| `decisions` | absolute path | Optional; JSON Decisions file |

## Output stream

Stdout contains only JSON Lines (one event per line).
Stderr contains human-readable diagnostics.
Do not parse stderr.

## Exit codes

| Code | Terminal outcome |
|---|---|
| 0 | `passed` |
| 1 | `infrastructure_error` |
| 2 | Usage or contract error (no terminal JSON event emitted) |
| 3 | `needs_decision` |
| 4 | `failed_retryable` |
| 5 | `failed_terminal` |
| 130 | `canceled` |

The JSON `run.completed` event is authoritative for outcomes 0, 1, 3, 4, 5, 130.
Exit code 2 indicates argument or contract errors before any events are emitted.

## Terminal outcomes

| Outcome | When | Next action |
|---|---|---|
| `passed` | All stages passed | Consigliere proceeds to delivery |
| `needs_decision` | Ask-user finding with no Decision | Consigliere asks human; rerun with Decisions file |
| `failed_retryable` | Auto-fixable finding, test/lint failure | Consigliere schedules repair Attempt |
| `failed_terminal` | Blocking finding or rejected Decision | Consigliere notifies; no repair |
| `infrastructure_error` | Config hash mismatch, workspace mutation, etc. | Consigliere quarantines workspace |
| `canceled` | Signal or context cancellation | Consigliere retries or aborts Mission |

## Evidence locations

Evidence is written to `<evidence-dir>/<hashed-run-id>/<invocation-id>/`:

```
<evidence-dir>/
  <hashed-run-id>/             (SHA-256 of run_id, lowercase hex)
    <invocation-id>/           (unique per invocation; lowercase hex)
      review/
        findings.json          — agent response and structured findings
      test/
        stdout.log             — test stage output
        stderr.log             — test stage errors
      document/
        findings.json          — documentation findings
      lint/
        stdout.log             — lint stage output
        stderr.log             — lint stage errors
      terminal.json            — run summary and outcome
```

Evidence paths are relative to `<evidence-dir>` and must be followed from the events.
`<invocation-id>` allows multiple Made invocations (reruns) to share the same hashed run
directory while isolating evidence by invocation instance.

Evidence is available after the process exits with any code ≥ 0.

## Cancellation behavior

Send SIGTERM or SIGINT. Made emits one `run.completed` event with outcome
`canceled` and exits 130. Evidence collected before cancellation is preserved.
The workspace state after cancellation is undefined; treat it as potentially dirty.

## Subprocess and environment isolation (Blocker 6 requirement)

**Critical requirement for security**: `made validate --managed` must execute in an
unprivileged, isolated process environment. The process must NOT inherit sensitive
credentials or configuration from the host environment.

### Required isolation boundaries

The validator process MUST be confined to:

- **Filesystem**: Read-only access to `trusted_config` and workspace; writable only to `evidence_dir`
- **Environment**: Only essential build environment variables (e.g., `PATH`, `HOME` pointing to a temporary sandbox)
- **Secrets**: No access to Made daemon state, delivery credentials, GitHub credentials, or any production secrets
- **Network**: No network access unless explicitly required for build/test commands
- **Privileges**: Non-root; no special capabilities; no capability escalation

### Why this matters

Test and lint stages execute candidate-written code in the workspace. A malicious or
compromised candidate can cause test processes to:

- Read environment variables containing credentials (GitHub tokens, API keys)
- Exfiltrate code or secrets to external services
- Access other workspaces or Made daemon state
- Modify or escape the intended sandbox

### Implementation responsibility

**Option A (Recommended)**: Consigliere enforces isolation
- Run Made process in a containerized or VM-based validator sandbox
- Mount only necessary paths with correct permissions
- Supply only non-sensitive environment variables
- Example: `docker run --rm -v workspace:/ws -v evidence:/evidence -v trusted-config:/config:ro -- made validate --managed ...`

**Option B**: Made enforces environment allowlist
- Made validates or filters the process environment before spawning test/lint subprocesses
- Not recommended; Consigliere has better visibility into process context

Consigliere should prefer **Option A**: establish the sandbox boundary before invoking Made,
ensuring isolation is the default behavior independent of Made changes.

## Version negotiation

Check `made capabilities --json`. The `commands` array contains `"validate.managed.v1"` when managed validation is supported.

```bash
made capabilities --json | jq '.commands | contains(["validate.managed.v1"])'
```

## Opaque fields

The following fields are echoed exactly as supplied and are never interpreted by Made:

- `run_id`
- `mission_id`

## Values that must be exact

- `input_sha` — must exactly match workspace HEAD (full 40-hex)
- `policy_hash` — must exactly match SHA-256 of `trusted_config` bytes (lowercase hex)
- Decisions file `run_id`, `mission_id`, `input_sha`, `policy_hash` — must match CLI flags

## What Made never does

In managed mode, Made never:

- Creates commits or applies patches
- Pushes to any remote
- Creates pull requests
- Monitors CI
- Merges
- Waits for human input
- Reads workspace `.made.yml`
- Contacts the Made daemon
- Creates durable run records
- Writes inside the workspace directory

## Rerunning with a Decisions file

To resolve `needs_decision`:

1. Extract unresolved findings from the `run.completed` payload `findings` array
2. Collect boss Decisions (approved/rejected) for each finding fingerprint
3. Write a Decisions JSON file (see schema below)
4. Re-invoke `made validate --managed` with the same arguments plus `--decisions`

The `input_sha` and `policy_hash` in the Decisions file must match the original run.
A Decisions file from a different SHA is rejected at preflight.

### Decisions file schema

```json
{
  "schema_version": 1,
  "run_id": "G-229",
  "mission_id": "M-402",
  "input_sha": "2222222222222222222222222222222222222222",
  "base_sha": "1111111111111111111111111111111111111111",
  "invocation_id": "0987654321fedcba",
  "policy_hash": "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
  "decisions": [
    {
      "decision_id": "D-184",
      "finding_fingerprint": "sha256:1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
      "outcome": "approved",
      "scope": "sha_bound",
      "rationale": "Accepted for this validation input"
    }
  ]
}
```

Supported `outcome` values: `approved`, `rejected`  
Supported `scope` values: `one_shot`, `sha_bound`, `mission_finding_waiver`

Made validates and echoes scope metadata; Consigliere owns whether a Decision was authorized.
