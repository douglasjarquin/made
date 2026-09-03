# Made feature map

This index is a trusted Review guide (`review.guides`, issue #40). It names
the major capabilities and the packages that own them. Follow only the
entries relevant to the exact base-to-input change unless the change is
broad enough to require a full sweep.

## Daemon-backed gate pipeline

The local Consigliere/push path. `made gate init` installs hooks; a push
submits an exact SHA to the singleton daemon. `internal/orchestrator`
chains the nine pipeline stages against a worktree of that SHA. This path
can push, open a PR, and poll CI; it never merges.

Entry points: `cmd/made/gate.go`, `cmd/made/daemon.go`,
`cmd/made/runcommands.go`, `internal/daemon`, `internal/orchestrator/workfunc.go`.

## made validate --managed

A generic, short-lived, daemonless engine for one exact input SHA. It
derives a policy-only stage plan (`internal/managed/stageplan.go`) before
anything runs. Review arrives through a `ReviewSource`
(`internal/managed/reviewsource.go`): `internal` spawns Made's own agent,
`external` accepts one caller-supplied result validated against
`internal/managed/contractreview.go`. It never pushes, creates a PR, polls
CI, or merges.

Entry points: `cmd/made/validate.go`, `internal/managed` (see
`docs/managed-validation-v1.md`).

## made verify

A friendlier daemonless command surface over the managed engine (issue
#41). `made verify [run]` is the internal one-shot; `made verify prepare` /
`made verify complete` split external review around a hash-bound request.
`base_sha` is the local merge-base with `--base-ref` (never fetched).
Trusted config is the current worktree's discovered file — a documented V1
simplification, not a git-ref trusted/candidate split. Temporary state lives
under `os.UserCacheDir()/made/verify/<hash of repo root>`, never inside
`.made/` or `.git/`.

Entry points: `cmd/made/verify.go`, `internal/verify`.

## made cursor

Generates two project-local Cursor projections from trusted config (issue
#42) so a Cloud agent can drive `made verify prepare`/`complete` with no
Made-launched daemon, gate, or review harness. `.cursor/agents/made-reviewer.md`
is written only when `review.executors.cursor.model` is set; its
frontmatter copies that exact model, never `inherit`.
`.cursor/skills/verify-with-made/SKILL.md` is always generated.
`made cursor init`/`sync`/`check`/`doctor` maintain them. Generated files
carry `cursor.GeneratedMarker`; unmarked files are not overwritten without
`--adopt`.

Entry points: `cmd/made/cursor.go`, `internal/cursor`.
