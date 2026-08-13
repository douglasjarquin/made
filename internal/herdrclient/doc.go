// Package herdrclient is a Go client for herdr's own JSON-RPC socket API
// (a separate program and protocol from made's own internal/api). It exists
// only to give a gate run an optional live-visibility pane in herdr; herdr
// is never a trust boundary or execution channel for made.
//
// Every call made through this package passes an explicit session name
// (SessionName) and never reads the ambient HERDR_SESSION environment
// variable, per the isolation contract enforced elsewhere for herdr
// consumers (see consigliere's cs-brief.sh). Protocol compatibility with
// the connected herdr server is checked with an exact-version match; a
// mismatch degrades to StateIncompatible rather than a hard error, since
// callers are expected to fail open when herdr isn't available or usable.
package herdrclient
