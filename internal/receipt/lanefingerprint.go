package receipt

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os/exec"
	"runtime"
	"sort"

	"github.com/douglasjarquin/made/internal/api"
)

// LaneFingerprintInputs bundles everything BuildLaneFingerprint needs to
// compute one validation-lane Full command's Fingerprint (project issue #33
// Phase 3). Both the daemon-backed pipeline and the read-only managed/verify
// reuse check call BuildLaneFingerprint directly, since a receipt published
// by one must be found by the other only when every field matches exactly.
type LaneFingerprintInputs struct {
	RepoIdentity string
	BaseSHA      string
	CandidateSHA string
	ConfigHash   string
	LaneName     string
	MatchedPaths []string
	Command      []string
	// MadeVersion is the caller's own made-version identifier: the
	// daemon-backed pipeline and the managed engine track this
	// independently (see internal/managed.MadeVersion), so it is supplied
	// by the caller rather than hardcoded here.
	MadeVersion string
}

// BuildLaneFingerprint constructs the Fingerprint for one validation-lane
// Full command.
func BuildLaneFingerprint(in LaneFingerprintInputs) Fingerprint {
	return Fingerprint{
		SchemaVersion:    FingerprintSchemaVersion,
		Lane:             in.LaneName,
		ValidationLevel:  "full",
		RepoIdentity:     in.RepoIdentity,
		BaseSHA:          in.BaseSHA,
		CandidateSHA:     in.CandidateSHA,
		ConfigHash:       in.ConfigHash,
		Command:          in.Command,
		WorkingDirectory: ".",
		InputSetHash:     hashPathSet(in.MatchedPaths),
		ToolchainHash:    ToolchainFingerprint(),
		OS:               runtime.GOOS,
		Arch:             runtime.GOARCH,
		MadeVersion:      in.MadeVersion,
		ProtocolVersion:  api.Version,
	}
}

func hashPathSet(paths []string) string {
	sorted := append([]string(nil), paths...)
	sort.Strings(sorted)
	data, err := json.Marshal(sorted)
	if err != nil {
		// paths is always a []string; Marshal cannot fail for that shape.
		panic("receipt: path set is not JSON-marshalable: " + err.Error())
	}
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// ToolchainFingerprint is a coarse, best-effort proxy for "the Go toolchain
// that would build/test this candidate": the exact output of `go version`.
// It says nothing about installed system libraries, other language
// toolchains, or transitive tool versions - refining it is left to a later
// iteration once basic reuse is proven safe. Any failure to even run `go
// version` yields a fixed sentinel, so a fingerprint is still well-defined
// (and simply never matches a receipt from an environment where it did
// succeed).
func ToolchainFingerprint() string {
	out, err := exec.Command("go", "version").Output()
	if err != nil {
		return "unknown"
	}
	sum := sha256.Sum256(out)
	return "sha256:" + hex.EncodeToString(sum[:])
}
