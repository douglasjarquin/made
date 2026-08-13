---
name: made
description: Validate your code changes through the made pipeline - a 9-stage GitHub-only gate (intent, rebase, review, test, document, lint, push, PR, CI) that runs as a push to a local bare gate repo and reports structured JSON status. Use when the user asks to run made, gate or validate their changes, push safely, asks you to do a task and then validate it, or invokes /made.
user-invocable: true
---

# made

`made` is a local validation-gate daemon: pushing a branch to its bare gate
repo runs a 9-stage pipeline (Intent, Rebase, Review, Test, Document, Lint,
Push, PR, CI) against the change before it ever reaches the real remote. It is
GitHub-only (via `gh`) and drives Claude or Codex as the pipeline's review
and document agent. You drive it through the `made` CLI, which talks to a
per-user background daemon over a unix socket and reports state as JSON.

## Two ways to invoke

`/made` works in two modes, depending on whether the user hands you a task
along with the command:

- **Validate-only** - bare `/made`. The user's code changes are already
  committed; push them through the gate and report the outcome.
- **Task-first** - `/made <task>`, e.g. `/made add a --json flag to the status command`.
  First carry out the task yourself, commit it, then validate:
  1. **Check scope.** Inspect `git status` before you change or commit
     anything, and commit only the changes that belong to the task.
  2. **Do the work**, then commit it on a branch other than the repository's
     default branch - the gate validates a pushed branch, and pushing the
     default branch to the gate is not a supported flow.
  3. **Then validate** by pushing that branch to the gate remote.

## Before you start

- The repository must already have a gate: `made gate init` creates the
  bare gate repo and a `made` git remote pointing at it, once per repo.
- The work you want validated must be **committed**; the gate validates
  pushed history, not your uncommitted working tree.
- The daemon starts on demand, but `made doctor` is the fastest way to
  confirm the daemon, `gh` authentication, and (optionally) a running herdr
  server are all reachable before you push.

## Running the gate

```sh
made gate init                # once per repo: create the bare gate + remote
git push made <branch>        # run all 9 stages against <branch>
made status --json            # structured run state, per-stage results,
                               # and any pending ask-user findings
```

The push blocks until the pipeline reaches a terminal state or parks on a
finding that needs a human decision. Use `made status --json` from a
separate call to check progress without disturbing the run.

## Findings and approval

Review and Document can surface findings while the pipeline runs:

- Auto-fixable findings are applied automatically as new commits; nothing to
  do.
- **ask-user** findings are queued, never silently applied or dropped. Run
  `made review` to see them and approve or reject each one from a plain
  stdin/stdout prompt - relay the finding to the user verbatim rather than
  paraphrasing it, since it challenges something about their intent or the
  product behavior of the change.

A rejected finding halts the pipeline at that stage; an approved one applies
and the run resumes.

## What each stage does

1. **Intent** - requires a stated goal for the change before anything else
   runs.
2. **Rebase** - rebases the pushed branch onto the trusted default branch,
   halting on a real conflict rather than guessing a resolution.
3. **Review** - the configured agent reviews the diff and raises findings.
4. **Test** - runs the repository's test command; a failing test blocks the
   push outright.
5. **Document** - checks the change against the repo's documentation policy
   and raises findings the same way Review does.
6. **Lint** - runs the repository's lint command.
7. **Push** - pushes the validated branch to the real remote. This stage
   never merges anything.
8. **PR** - opens a GitHub pull request for the branch and stops; made has no
   merge-capable code path, so it structurally cannot merge on your behalf.
9. **CI** - watches the PR's checks and reports `checks-passed` once they
   are green, without waiting for a human to merge.

## Outcomes

- `checks-passed` - validated and CI is green, but the PR is not merged.
  Tell the user it is ready for their review; do not wait for the merge
  yourself.
- `passed` - the PR was merged or closed after checks passed.
- `failed` or `cancelled` - something blocked the run. Read
  `made status --json` for the failing stage, fix it, commit the fix on
  the same branch, and push to `made` again.

## herdr visibility

If a herdr server is reachable, made opens a pane there so a human can watch
the run live. This is visibility only - herdr never creates the gate's
worktree and never decides pass or fail - so a run completes normally even
when no herdr server is running.
