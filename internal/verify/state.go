package verify

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
)

func StateRoot(canonicalRoot string) string {
	base, err := os.UserCacheDir()
	if err != nil || base == "" {
		base = os.TempDir()
	}
	sum := sha256.Sum256([]byte(canonicalRoot))
	id := hex.EncodeToString(sum[:])[:16]
	return filepath.Join(base, "made", "verify", id)
}

func RequestsDir(stateRoot string) string  { return filepath.Join(stateRoot, "requests") }
func EvidenceRoot(stateRoot string) string { return filepath.Join(stateRoot, "evidence") }
func ReceiptsDir(stateRoot string) string  { return filepath.Join(stateRoot, "receipts") }
