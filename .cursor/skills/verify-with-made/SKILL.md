---
name: verify-with-made
description: Complete Made's exact-SHA verification (made verify) for the current committed change, using an external Cursor reviewer subagent when Made Review is configured for Cursor.
---

# verify-with-made

This skill is an operating adapter over Made's existing `made verify`
command surface (project issue #41). It never re-implements validation
itself: `made verify prepare`/`made verify complete` do the actual work; this
skill only sequences them correctly from inside a Cursor Cloud coding agent
turn. No daemon, gate, push, pull request, or CI polling is ever involved.

## Before you start

Made must already be installed and on PATH before this skill runs - prefer a
pinned Made release (`made capabilities --json` should succeed) installed during
the Cloud environment build; see "Installation" below. Do not clone,
`go install`, or rebuild Made from source on every skill invocation.

## Steps

1. **Confirm the worktree is clean.** Every change you intend to verify must
   already be committed on the current branch. Run `git status --porcelain`
   and commit or discard anything unexpected - Made verifies committed
   history, not your working tree.
2. **Run `made cursor doctor --base-ref <trusted-ref> --json`.** Confirm
   `healthy` is `true` before continuing. Made never needs a daemon or gate
   for this flow, so a failing check here means a real configuration or
   environment problem, not a missing daemon. Passing `--base-ref` here (the
   same ref you pass to `made verify prepare` below) is what makes the
   `base_ref` check actually run instead of reporting `skipped`.
3. **Prepare the request.**

   ```sh
   made verify prepare --executor cursor --base-ref <trusted-ref> --json
   ```

   `<trusted-ref>` is the branch this candidate is measured against (e.g.
   `origin/main`); Made resolves `base_sha` as the local merge-base with the
   current HEAD and never fetches. The JSON response's `request_path` names
   the file you hand to the reviewer subagent next.
4. **Branch on the `cursor_executor` check's `detail` field** from step 2's
   doctor report (`status` is always `ok`/`warn`/`skipped` here; the
   `configured`/`not_configured` strings live in `detail`):
   - `detail` is `configured`: invoke the `made-reviewer` subagent with the
     exact contents of `request_path` as its only input. Do not summarize,
     paraphrase, or add anything to the request before handing it over. If
     your harness has no built-in mechanism to invoke a named custom
     `.cursor/agents/*.md` subagent file, read `made-reviewer.md`'s
     frontmatter and body yourself, and launch a fresh subagent whose entire
     system prompt is that body verbatim, then give it the prepared request
     as its input. This does not license paraphrasing the request itself -
     only the request content must stay byte-for-byte unmodified.
   - `status` is `warn` (`review.required` is true but
     `review.executors.cursor.model` is unset): stop here and report the
     doctor warning. Do not fall through to either the Cursor-review or
     no-review path silently.
   - `detail` is `not_configured`: skip steps 3-6 entirely and go to step 7.
5. **Save the reviewer's output verbatim.** The reviewer returns one strict
   JSON document (project issue #39's external Review schema). Write it,
   byte-for-byte, to a result file. Never edit, wrap, reformat it, or convert
   its prose into findings yourself - Made's own parser is the only thing
   that interprets it.
6. **Complete verification.**

   ```sh
   made verify complete --request <request_path> --review-result <result_path> --json
   ```

   Made re-validates the reviewer's result against the exact prepared
   contract, runs the remaining configured Test/Document/Lint stages, and
   writes an exact-HEAD receipt.
7. **If Review is not configured for Cursor**, skip steps 3-6 entirely and
   run:

   ```sh
   made verify run --base-ref <trusted-ref> --json
   ```

   This truthfully runs whatever Test/Document/Lint stages the trusted
   configuration defines. Do not invent a Review result, and do not invoke
   `made-reviewer` when it was never requested.
8. **On `failed_retryable`:** fix only the findings the receipt actually
   reports, commit the fix as a new SHA, and start a fresh cycle at step 1.
   Never reuse a request, result, or receipt from the prior SHA - each
   `made verify prepare` binds to exactly one `input_sha`.
9. **On `needs_decision`:** stop. Surface the exact decision required; this
   skill has no authority to record a Decision on your behalf.
10. **On `failed_terminal`, `infrastructure_error`, or `canceled`:** report the
    receipt's exact `message`, `stopped_at` stage, and evidence references.
    Do not retry blindly or guess at a fix the evidence doesn't support.
11. **On `passed`:** report the exact `input_sha`, Review provenance (`source`,
    `executor`, `requested_model`, `actual_model`), the guides supplied, per-stage
    coverage, and the receipt's location.
12. **Never**, at any point in this flow: start the Made daemon, run
    `made gate init`, push a branch, open a pull request, poll CI, or merge
    anything. This flow has no such command in it precisely so it can't.
13. **Never reuse** a request, result, or receipt bound to an earlier SHA,
    even if the change looks trivial - always run `made verify prepare`
    again for the current HEAD.

## Retry loop

Every `made verify prepare` binds one exact `input_sha` to one request; every
`made verify complete` re-validates that exact binding before running
anything. A new commit always means: retire the old request/result, run
`made verify prepare` again for the new SHA, and get a fresh `made-reviewer`
subagent context - never patch an old request or reuse an old reviewer
result against a new commit.

## Installation

Until Made ships pinned release automation (project issue #27), run
`scripts/install-cursor-cloud.sh` during the Cloud environment build
step, before any agent turn starts. It builds a pinned Made binary at an
exact commit SHA (so receipts report that real SHA as `made_version`,
never `dev`) and installs the exact `golangci-lint` version this
project's CI uses, both onto PATH. Do not run it on every skill invocation.
Once release automation exists, the intended flow is: the Cloud Build
downloads and verifies a pinned Made binary, places it on PATH before the
agent turn begins, and `made capabilities --json` proves the required
interfaces are present.

<!-- Generated by Made from project config; run `made cursor sync`. -->
