package evidence

import (
	"fmt"
	"path/filepath"
	"strings"
)

const (
	DefaultBranch        = "made-evidence"
	DefaultDir           = ".made/evidence"
	maxEvidenceFileBytes = 1 << 20
	maxEvidenceBytes     = 4 << 20
)

type Config struct {
	StoreInRepo    bool
	Dir            string
	Branch         string
	RetentionBytes int
}

func validateEvidenceInput(runID string, files map[string][]byte, retentionBytes int) error {
	cleanRunID := filepath.Clean(runID)
	if runID == "" || cleanRunID != runID || filepath.IsAbs(runID) || cleanRunID == "." || cleanRunID == ".." || strings.HasPrefix(cleanRunID, ".."+string(filepath.Separator)) {
		return fmt.Errorf("evidence: invalid runID %q", runID)
	}
	total := 0
	if retentionBytes <= 0 {
		retentionBytes = maxEvidenceBytes
	}
	for name, data := range files {
		clean := filepath.Clean(name)
		if name == "" || filepath.IsAbs(name) || clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) || clean == ".git" || strings.HasPrefix(clean, ".git"+string(filepath.Separator)) {
			return fmt.Errorf("evidence: path %q escapes run evidence directory", name)
		}
		if len(data) > maxEvidenceFileBytes || total+len(data) > retentionBytes {
			return fmt.Errorf("evidence: retention limit exceeded for %q", name)
		}
		total += len(data)
	}
	return nil
}

type Store interface {
	WriteEvidence(runID string, files map[string][]byte) error
}

type Publisher interface {
	PublishEvidence(runID string) error
}

func NewStore(repoPath string, cfg Config) Store {
	if cfg.StoreInRepo {
		return &InRepoStore{RepoPath: repoPath, Dir: cfg.Dir, RetentionBytes: cfg.RetentionBytes}
	}
	return &OrphanBranchStore{RepoPath: repoPath, Branch: cfg.Branch, RetentionBytes: cfg.RetentionBytes}
}
