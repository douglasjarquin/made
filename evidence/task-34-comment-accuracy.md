=== New failing-first test: tests/cs-classify-lib-made-comments.test.sh ===
--- RED (before edits, git stash of bin/cs-classify-lib.sh) ---
exit: 1

--- GREEN (after edits) ---
ok - bin/cs-classify-lib.sh has no stale no-mistakes references
ok - bin/cs-classify-lib.sh's busy-detection comments name made, not no-mistakes
exit: 0

=== grep -n no-mistakes bin/cs-classify-lib.sh (must be empty) ===
exit: 1

=== shellcheck -x bin/cs-classify-lib.sh ===
exit: 0

=== Sibling test suites that exercise cs-classify-lib.sh (no dedicated cs-classify-lib.test.sh exists) ===
--- tests/cs-crew-state.test.sh ---
not ok - active run -> working (missing: 'state: working')
--- output ---
state: unknown · source: none · no current-state source available
exit: 0

--- tests/cs-decision-hold.test.sh ---
ok - non-forced scout teardown always requires durable inventory verification
ok - boss holds are idempotent, distinct, teardown-safe, and durably routed before close
ok - completion and verification validate origins before constructing paths
ok - ended visual review follows the same decision-hold completion owner
ok - resolved findings and decision-like prose do not create false holds
ok - terminal single-owner stale status decisions do not block empty inventory
ok - main-home and capo-home boss holds remain correctly routed
ok - resolve matches first/middle/last in quoted blocked_by and rejects a genuinely absent id
exit: 0

--- tests/cs-open-decision-cursor.test.sh ---
ok - a same-size replacement is caught by the device+inode identity check
ok - a read failure reports the trusted set unchanged and preserves the cursor
ok - a chunk staging failure answers from the full fold and preserves the cursor
ok - a failed staged cursor write preserves the previous cursor
ok - atomic cursor temp files ignore predictable symlinks
ok - compare-before-rename skips an observed newer cursor
ok - deleting the cursor at any time only costs one full re-fold
ok - scan_open_decisions_incremental matches scan_open_decisions cold and warm
exit: 0

--- tests/cs-operational-input.test.sh ---
ok - operational-input library and CLI are source-safe and round-trip every kind
ok - bare away and labeled from-consigliere marker bytes remain exact
ok - shared classifier returns structural kinds and preserves unmarked boss input
ok - session-start digest carries the session-start kind
ok - turn-end Stop-hook continuation carries the turn-end-guard kind
ok - spawn passes a typed launch-brief prompt on every launch path
ok - canonical operational-input behavior
exit: 0

--- tests/cs-send-resolve-key.test.sh ---
ok - cs-send: --resolve-key refuses --key, pane targets, empty answers, and bad keys
ok - cs-send: a failed close reports the manual command and leaves the decision open
ok - cs-send: an over-long answer is cut to the shared per-line cap
ok - cs-send: a queued mid-turn answer closes its decision
ok - cs-send: a quoted corr token never lands in the parent-written closing line
ok - cs-send: the cap cuts the note only and a long key's close still parses
ok - cs-send: an over-long key is refused before the send
ok - cs-send --resolve-key answerer-closes contract
exit: 0

--- tests/cs-wake-open-decisions.test.sh ---
ok - a resolved capo escalation leaves the OPEN DECISIONS section
ok - a resolved decision is folded out and the clean drain stays quiet
ok - non-empty drain keeps raw rows and annotations and adds the section
ok - unreadable and symlinked status files are skipped without stderr noise
ok - a clean drain prints no OPEN DECISIONS section
ok - unset, empty, and non-positive caps fall back to the default
ok - scan_open_decisions folds every task and drops resolved decisions
ok - status_open_decisions skips symlinked and unreadable files silently
exit: 0

--- tests/cs-watch-triage.test.sh ---
ok - capo worker events dedupe on the surfaced line and skip unmarked homes
ok - a capo decision survives a later working: append (the exact overnight bug class)
ok - a resolved capo decision does not resurface
ok - identically-worded decisions on different tasks each get their own marker and both surface
ok - a decision reopened under the same key and wording after a resolve is treated as a new surfacing event
ok - the splice handles exited and output kinds and ignores an unknown one
ok - the per-cycle snapshot answers pane status without a per-pane query
ok - a pane absent from the snapshot falls back to a direct query
exit: 0

=== cs-crew-state.test.sh failure pre-exists independent of this task's edit (confirmed via git stash of bin/cs-classify-lib.sh) ===
not ok - active run -> working (missing: 'state: working')
--- output ---
state: unknown · source: none · no current-state source available
