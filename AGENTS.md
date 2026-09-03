# Project agent memory

This file is the project's committed home for project-intrinsic agent knowledge: build, test, release, architecture, and sharp-edge notes that should travel with the code.

- Add durable project-specific notes here as they are discovered through real work.

- The versioned runtime contract is implemented at `cmd/made/runcommands.go` and `internal/daemon`; validate it through `made capabilities --json` and the `made run ... --json` commands.
- Config lives at `.made.yaml` or `.made/config.yaml` (equally valid, conflict if both present); legacy root `.made.yml` still works during a bounded deprecation window. All three are decoded strictly with `version: 1`. The sole discovery entry point is `config.Locate` in `internal/config/locate.go`; the loader is `internal/config/file.go`.
- Local git fixtures can inherit SSH commit signing from the host; use process-local `GIT_CONFIG_COUNT=1 GIT_CONFIG_KEY_0=commit.gpgsign GIT_CONFIG_VALUE_0=false` when running the Go test suite.
- `made validate --managed --json-events` (`internal/managed`) is a generic, short-lived, daemonless validation engine for one exact input SHA - see `docs/managed-validation-v1.md` and `docs/managed-validation-integration.md`. It derives a policy-only stage plan (`internal/managed/stageplan.go`) before running anything: Review/Test/Document/Lint each resolve to `run`, `not_configured`, or `disabled`, and a plan with no `run` stage fails as `infrastructure_error` rather than a meaningless pass. Review is obtained through a `ReviewSource` (`internal/managed/reviewsource.go`): `internal` spawns Made's own agent in report-only mode, `external` accepts one caller-supplied result validated by `internal/managed/externalreview.go` against `internal/managed/contractreview.go`'s canonical, hash-bound review contract - both sources feed the same finding/fingerprint/Decision classification path in `internal/managed/runner.go`. It is daemonless and can never push, create a PR, poll CI, or merge; those remain the standalone daemon-backed pipeline's job (unchanged, see `internal/orchestrator`).
- `review.guides` (project issue #40, `internal/config/config.go`'s `Review.Guides`) lets a project point Review at zero or more trusted, repository-relative guide files (a feature map is the recommended convention, not a dedicated subsystem). Managed mode resolves and hashes them from the trusted root in `internal/managed/guides.go` (`TrustedGuideRoot`/`ResolveTrustedGuides`) before Review runs, and binds path/hash/bytes into `ReviewContract.Guides` (`internal/managed/contractreview.go`) so its hash changes whenever a guide's order, path, or content changes. The trusted root today is derived only from `--trusted-config`'s own filesystem location - there is no daemon-mediated trusted-base-at-a-git-ref yet (that is `made verify`'s job, issue #41). Both `InternalReviewSource` (via `internal/pipeline/review` and `internal/agent`'s `ReviewGuideRef`/`ReviewGuide`) and `ExternalReviewSource` receive the identical guide list; an external result may optionally echo bounded `guides_consulted` metadata, validated in `internal/managed/externalreview.go`.

## Maintaining this file

Keep this file for knowledge useful to almost every future agent session in this project.
Do not repeat what the codebase already shows; point to the authoritative file or command instead.
Prefer rewriting or pruning existing entries over appending new ones.
When updating this file, preserve this bar for all agents and keep entries concise.
