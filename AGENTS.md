# Project agent memory

This file is the project's committed home for project-intrinsic agent knowledge: build, test, release, architecture, and sharp-edge notes that should travel with the code.

- Add durable project-specific notes here as they are discovered through real work.

- The versioned runtime contract is implemented at `cmd/made/runcommands.go` and `internal/daemon`; validate it through `made capabilities --json` and the `made run ... --json` commands.
- `.made.yml` is decoded strictly with `version: 1`; the authoritative loader is `internal/config/config.go`.
- Local git fixtures can inherit SSH commit signing from the host; use process-local `GIT_CONFIG_COUNT=1 GIT_CONFIG_KEY_0=commit.gpgsign GIT_CONFIG_VALUE_0=false` when running the Go test suite.

## Maintaining this file

Keep this file for knowledge useful to almost every future agent session in this project.
Do not repeat what the codebase already shows; point to the authoritative file or command instead.
Prefer rewriting or pruning existing entries over appending new ones.
When updating this file, preserve this bar for all agents and keep entries concise.
