# F4 Scope Fidelity Check — Independence Spot-Check

## gitgate vs no-mistakes/internal/git

**File layout**: made splits into `bare.go`, `hook.go`, `layout.go`, `worktree.go` (4 files, one concern each). no-mistakes concentrates bare-repo logic in a single `git.go` plus `hook.go`/`env.go`. Different decomposition.

**Naming**: made uses `InitBare`, `InstallAdmissionHook`, `AddWorktree`, `GatePath`, `WorktreesDir` — no-mistakes uses `RunBare`, `ValidateBareRepository`, `InitBare` (name coincidentally overlaps on this one common Git-domain term, rest differ). made's admission hook is a shared-secret token-file check (`internal/gitgate/hook.go`), independently designed and explicitly documented as a placeholder for the daemon-socket auth wired in later — no-mistakes' pre-receive hook does daemon-process authentication directly (`internal/git/hook.go:17-85`). Different mechanisms, not a port.

**No copied comments or identifiers found.**

## api vs herdr/src/api

**File layout**: made has `client.go`, `envelope.go`, `server.go` (3 files, minimal surface: `ping`, `status`, `review.decide`, `review.decision`). herdr has 8 files (`schema.rs`, `server.rs`, `client.rs`, `status.rs`, `event_hub.rs`, `subscriptions.rs`, `wait.rs`, `mod.rs`) implementing a much larger pane/workspace/tab API with pub/sub event streaming.

**Idiom mirrored, not wire format**: both use a method+params+id request envelope and an exact-match integer protocol-version check (herdr's `PROTOCOL_VERSION`/`check_client_version` in `wire.rs:16,1009-1021`; made's `Version` const and `Server.dispatch`'s exact-match rejection in `envelope.go`/`server.go`). The actual JSON field names, Go types, and error-code taxonomy are made's own — not translated Rust structs. made never implements herdr's pane/workspace RPC surface at all (that lives in `internal/herdrclient`, a *client* of herdr's real protocol, which is necessarily wire-compatible with herdr since it has to talk to the real herdr binary — a different, correctly-scoped exception).

## Verdict

Independent structure confirmed at both the file-layout and naming level. No copied code, identifiers, or comments found. The one necessary exception (`internal/herdrclient` matching herdr's real wire format) is by design — it's a client for an external program's actual protocol, not a copy of herdr's own server-side implementation.
