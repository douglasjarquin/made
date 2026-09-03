// Package verify implements `made verify`: a friendlier, daemonless
// one-shot and two-step external-review workflow for validating one
// committed change (project issue #41).
//
// It is a thin command-surface wrapper over internal/managed's engine
// (Run/Runner/StagePlan/ReviewSource) and internal/config's config.Locate
// discovery (project issue #38) and guide resolution (project issue #40).
// It never reimplements Review, Test, Document, Lint, findings, Decisions,
// or evidence, and it never touches internal/orchestrator, internal/gitgate,
// or the daemon socket: no code path here can start the daemon, initialize
// a gate, push, create a PR, poll CI, or merge.
package verify
