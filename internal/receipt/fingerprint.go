// Package receipt defines the content-addressed schema for a reusable
// successful local-validation result (project issue #33 Phase 3). This
// package only defines the schema and a durable read/write primitive - it
// makes no reuse decision and nothing in the pipeline consults it yet.
// Wiring "skip execution because a receipt exists" is a separate, more
// dangerous change (a false-positive reuse would be a silent false pass)
// staged for its own review once this foundation is proven.
package receipt

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// FingerprintSchemaVersion is bumped whenever Fingerprint's fields change in
// a way that must invalidate every previously computed hash.
const FingerprintSchemaVersion = 1

// Fingerprint identifies everything that can affect whether a validation
// lane's Full commands would produce the same result again. Any field
// changing must change Hash() - see fingerprint_test.go for the exhaustive
// per-field check. A result may only be reused when every one of these
// matches exactly; no field is optional or best-effort.
type Fingerprint struct {
	SchemaVersion    int      `json:"schema_version"`
	Lane             string   `json:"lane"`
	ValidationLevel  string   `json:"validation_level"`
	RepoIdentity     string   `json:"repo_identity"`
	BaseSHA          string   `json:"base_sha"`
	CandidateSHA     string   `json:"candidate_sha"`
	ConfigHash       string   `json:"config_hash"`
	Command          []string `json:"command"`
	WorkingDirectory string   `json:"working_directory"`
	InputSetHash     string   `json:"input_set_hash"`
	ToolchainHash    string   `json:"toolchain_hash"`
	OS               string   `json:"os"`
	Arch             string   `json:"arch"`
	MadeVersion      string   `json:"made_version"`
	ProtocolVersion  int      `json:"protocol_version"`
}

// Hash returns the fingerprint's content address: a sha256 digest of its
// canonical (field-order-stable) JSON encoding, prefixed for readability
// and to leave room for a future algorithm change.
func (f Fingerprint) Hash() string {
	data, err := json.Marshal(f)
	if err != nil {
		// Fingerprint's fields are all JSON-trivial (strings, a string
		// slice, ints); Marshal cannot fail for this shape.
		panic("receipt: fingerprint is not JSON-marshalable: " + err.Error())
	}
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}
