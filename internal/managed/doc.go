// Package managed implements the Made managed-validation mode.
//
// Managed mode is a short-lived, daemonless execution shape invoked by a
// caller or orchestrator (for example Consigliere, Cursor Cloud, or a CI
// job) to validate an immutable input commit SHA. It runs validation stages
// (review, test, document, lint), emits a versioned JSON event stream to
// stdout, and returns one terminal outcome. It never waits for human input,
// never applies patches, and never mutates the workspace.
package managed
