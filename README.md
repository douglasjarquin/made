# made

<img width="1983" height="793" alt="made-github-header" src="https://github.com/user-attachments/assets/a75a12db-ab6b-4e79-9029-585632a6cdd1" />

A personal Go rewrite of [no-mistakes](https://github.com/kunchenguid/no-mistakes)'s validation-gate pipeline, deeply integrated with [herdr](https://github.com/douglasjarquin/herdr) and [consigliere](https://github.com/douglasjarquin/consigliere).

made is an independent synthesis, not a dependency bundle or a one-to-one copy of any source.

See `plans/made-rewrite.md` for the full design and build plan.

## Versioned daemon contract

`made capabilities --json` reports the public protocol and command schema.

Use `made run submit --json --gate <bare-gate-path> --ref refs/heads/<branch> --old-sha <previous-head-sha> --input-sha <sha>` to create a run.

Use `made run status --json <exact-run-id>` for one run, `made run list --json --active` for the active batch, and `made run cancel --json <exact-run-id>` for idempotent cancellation.

Use `made review decide --json --stage <stage> --decision <approved|rejected> <exact-run-id>` for an exact review decision.

Use `made doctor --json` for the fixed health schema.

Run states are `queued`, `running`, `awaiting_review`, `awaiting_merge`, `succeeded`, `failed`, `canceled`, and `superseded`.

Run state is persisted in a fsync-backed local WAL, and gate submissions use an idempotent fsync-backed spool keyed by gate, ref, and input SHA.

The daemon acquires its singleton before touching the Unix socket path, removes only stale sockets, and refuses regular files, symlinks, and directories.
