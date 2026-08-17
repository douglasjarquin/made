# Phase 4 disposable manual-QA evidence

The scenario used only the task branch binary, a disposable Made home, and an
isolated named Herdr lab session.
No real project gate was initialized and no shared Made daemon was changed.

## Real Made binary and durable CLI state

Build command:

```text
qa_dir=$(mktemp -d /tmp/made-remediation-qa.XXXXXX)
go build -o "$qa_dir/made" ./cmd/made
```

Build result:

```text
/tmp/made-remediation-qa.Z8Vnit
```

The disposable daemon was started with:

```text
MADE_HOME=/tmp/made-remediation-qa.Z8Vnit /tmp/made-remediation-qa.Z8Vnit/made daemon start --idle-timeout=5m
```

Observed result:

```text
made daemon: started (pid 33848)
```

Command:

```text
MADE_HOME=/tmp/made-remediation-qa.Z8Vnit /tmp/made-remediation-qa.Z8Vnit/made capabilities --json
```

Observed result:

```json
{"schema_version":1,"protocol_version":1,"commands":["run.submit","run.status","run.list","run.cancel","review.decide","doctor"]}
```

Command:

```text
MADE_HOME=/tmp/made-remediation-qa.Z8Vnit /tmp/made-remediation-qa.Z8Vnit/made run submit --repo qa/repo --branch feature/qa --ref refs/heads/feature/qa --old-sha 1111111111111111111111111111111111111111 --input-sha 2222222222222222222222222222222222222222 --submission-id qa-submission-1 --gate /tmp/qa-gate --json
```

The real binary returned the exact queued identity before drain with
`run_id=run-1`, `state=queued`, the supplied input SHA, submission ID, gate
path, and all nine ordered pending stages.

The immediate exact-ID status remained `state=queued` with
`execution_finished=false` and the same identity fields, proving the public
surface spools work without claiming that remediation executed.

Command:

```text
MADE_HOME=/tmp/made-remediation-qa.Z8Vnit /tmp/made-remediation-qa.Z8Vnit/made run status --json run-1
MADE_HOME=/tmp/made-remediation-qa.Z8Vnit /tmp/made-remediation-qa.Z8Vnit/made run status --json run-does-not-exist
MADE_HOME=/tmp/made-remediation-qa.Z8Vnit /tmp/made-remediation-qa.Z8Vnit/made status --json
```

Observed invalid-boundary results:

```text
made run status: handler_error: status: no run "run-does-not-exist"
made: status is obsolete; use made run status <exact-run-id>
```

The daemon was stopped through the same disposable Made home, restarted, and
the exact `run-1` status was restored from durable state with
`state=queued` and the same SHA/submission identity.

## Doctor JSON

Command:

```text
MADE_HOME=/tmp/made-remediation-qa.Z8Vnit /tmp/made-remediation-qa.Z8Vnit/made doctor --json
```

Observed result:

```json
{"schema_version":1,"protocol_version":1,"healthy":true,"checks":{"daemon":"reachable","gate":"not_initialized","github":"authenticated","herdr":"unavailable"}}
```

Herdr is informational in the doctor report and did not affect the Made-only
run contract.

## Isolated Herdr lab

Every task-specific Herdr probe used the required helper and trailing named
session argument.

Command:

```text
HERDR_LAB_HELPER='/Users/douglasjarquin/.consigliere/capos/made/bin/cs-herdr-lab.sh'
HERDR_LAB_SESSION='cs-lab-made-remediation-9714-1438'
"$HERDR_LAB_HELPER" run "$HERDR_LAB_SESSION" status server
```

Observed result:

```text
status: running
version: 0.8.0
protocol: 20
compatible: yes
socket: /Users/douglasjarquin/.config/herdr/sessions/cs-lab-made-remediation-9714-1438/herdr.sock
```

The named session remains provisioned until final cleanup through the helper.

## Follow-up after lifecycle review correction

Source and test commit:
`d1dab7c73c3bdf678a668891c17a04d9c34b13c4`.

The real Made binary was rebuilt from that commit and rerun against a fresh
disposable home at `/tmp/made-remediation-qa-delivery.G9K78Z`.
The public `run submit` response and exact status both remained
`state=queued` with `execution_finished=false`.
The same queued identity survived a disposable daemon stop and restart.
`made status --json` still rejected with exit code 2, and `doctor --json`
returned `healthy=true` with `daemon=reachable`, `gate=not_initialized`,
`github=authenticated`, and `herdr=unavailable`.
The disposable daemon was stopped and its temporary home was moved to
recoverable temporary trash at
`/tmp/.made-remediation-qa-delivery-trash.made-remediation-qa-delivery.G9K78Z`
after the scenario.

## Final source candidate

Source and test commit:
`fdd8a7853053e9eb0efc099244c5296006c3605a`.

The current `./cmd/made` binary was built into a fresh disposable home at
`/tmp/made-remediation-qa-fdd-green.ugxcmL`.

The disposable daemon was launched in the background and became ready on its
own `daemon.sock`.

The real binary reported the expected capabilities, then `run submit --json`
returned `run-1` with the supplied repository, branch, ref, old SHA, input SHA,
submission ID, gate path, `state=queued`, `execution_finished=false`,
`current_stage=intent`, and the nine ordered pending stages.

The exact `run status --json run-1` and `run list --json` responses preserved
the same identity and lifecycle state.

`doctor --json` returned `healthy=true` with
`daemon=reachable`, `gate=not_initialized`, `github=authenticated`, and
`herdr=unavailable`.

The daemon was stopped and restarted through the same disposable home.
The exact `run-1` status after restart preserved the queued state,
`execution_finished=false`, all identity fields, and all nine pending stages.

The invalid public-boundary checks returned:

```text
made run status run-1 unexpected
exit=2: usage: made run status <exact-run-id> [--json]

made status --json
exit=2: made: status is obsolete; use made run status <exact-run-id>
```

The disposable daemon was stopped and its home was moved to recoverable
temporary trash at
`/tmp/.made-remediation-qa-fdd-green-trash.rat3CP/qa-home`.

## Final lifecycle candidate

Source and test commit:
`60420902ea5b1ed434f57c86ebb0e85be7be5281`.

The current `./cmd/made` binary was built into a fresh disposable home at
`/tmp/made-remediation-qa-604.8yLbcs`.

The real binary returned the expected capabilities and spooled `run-1` with
the supplied identity, `state=queued`, `execution_finished=false`, and all
nine ordered pending stages.

`made run cancel run-1 --json` returned `{"ok":true}`.
The exact status immediately after cancellation returned
`state=canceled`, `execution_finished=true`, the same identity fields, and
`error=context canceled`.

A second spooled `run-2` preserved its exact identity and queued state until
the disposable daemon was stopped.
Graceful daemon shutdown intentionally canceled that in-flight queued record;
after restart, exact `run-2` status restored `state=canceled`,
`execution_finished=true`, and the same identity and stage records.
This proves durable terminal-state recovery across the real binary restart
while respecting the daemon's shutdown cancellation contract.

`doctor --json` returned `healthy=true` with
`daemon=reachable`, `gate=not_initialized`, `github=authenticated`, and
`herdr=unavailable`.

The invalid public-boundary checks returned exit code 2:

```text
made run status run-2 unexpected
usage: made run status <exact-run-id> [--json]

made status --json
made: status is obsolete; use made run status <exact-run-id>
```

The disposable daemon was stopped and its home was moved to recoverable
temporary trash at
`/tmp/.made-remediation-qa-604-trash.qkeAfA/qa-home`.

## Final source candidate

Source and test commit:
`910fc54a98e7da644bc5e170281fd935e429692f`.

The current `./cmd/made` binary was built into a fresh disposable home at
`/tmp/made-remediation-qa-910.WcgMFi`.

The real binary returned a spooled `run-1` with exact repository, branch, ref,
old SHA, input SHA, submission ID, gate path, `state=queued`,
`execution_finished=false`, and all nine ordered pending stages.

`made run cancel run-1 --json` returned `{"ok":true}`.
The exact status immediately after cancellation returned
`state=canceled`, `execution_finished=true`, the same identity fields, and
`error=context canceled`.

After the disposable daemon stopped and restarted, exact `run-1` status
restored the same canceled terminal state and identity fields.

`doctor --json` returned `healthy=true` with
`daemon=reachable`, `gate=not_initialized`, `github=authenticated`, and
`herdr=unavailable`.

The invalid public-boundary checks returned exit code 2:

```text
made run status run-1 unexpected
usage: made run status <exact-run-id> [--json]

made status --json
made: status is obsolete; use made run status <exact-run-id>
```

The disposable daemon was stopped and its home was moved to recoverable
temporary trash at
`/tmp/.made-remediation-qa-910-trash.KmgO42/qa-home`.
