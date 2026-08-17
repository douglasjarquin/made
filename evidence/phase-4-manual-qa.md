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

The immediate exact-ID status returned `state=succeeded` and
`execution_finished=true` without changing the identity fields.

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
`state=succeeded` and the same SHA/submission identity.

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
