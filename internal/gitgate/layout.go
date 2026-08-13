package gitgate

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
)

func GatePath(madeHome, repoIdentifier string) string {
	sum := sha256.Sum256([]byte(repoIdentifier))
	hash := hex.EncodeToString(sum[:])
	return filepath.Join(madeHome, "gates", hash, "gate.git")
}

func WorktreesDir(gatePath string) string {
	return filepath.Join(filepath.Dir(gatePath), "worktrees")
}
