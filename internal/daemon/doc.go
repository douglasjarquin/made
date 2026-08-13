// Package daemon implements made's per-user singleton daemon: an exclusive
// file lock guarding against double-start, and a minimal start/stop/idle
// lifecycle. The run manager and event mailbox live here too, added later.
package daemon
