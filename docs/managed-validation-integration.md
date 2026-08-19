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

Evidence is written to:

```
<evidence-dir>/<run-id>/
  manifest.json      — run summary
  review/            — agent response and findings
  test/              — stdout.log, stderr.log
  document/          — findings
  lint/              — stdout.log, stderr.log
  terminal.json      — complete terminal summary
```

Evidence is available after the process exits with any code ≥ 0.

## Cancellation behavior

Send SIGTERM or SIGINT. Made emits one `run.completed` event with outcome
`canceled` and exits 130. Evidence collected before cancellation is preserved.
The workspace state after cancellation is undefined; treat it as potentially dirty.

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
  "policy_hash": "sha256:<64-hex>",
  "decisions": [
    {
      "decision_id": "D-184",
      "finding_fingerprint": "sha256:<64-hex>",
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
