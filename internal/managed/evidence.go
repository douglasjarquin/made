package managed

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// ManagedEvidenceStore writes evidence under <evidenceDir>/<safeRunID>/<invocationID>/.
// The safeRunID is derived from the run_id via SHA-256 to prevent path traversal.
// Each invocation gets its own subdirectory, preserving evidence from prior runs.
type ManagedEvidenceStore struct {
	EvidenceDir  string
	RunID        string
	InvocationID string
	safeRunID    string
}

// NewManagedEvidenceStore constructs a store bound to a specific invocation.
func NewManagedEvidenceStore(evidenceDir, runID, invocationID string) *ManagedEvidenceStore {
	sum := sha256.Sum256([]byte(runID))
	return &ManagedEvidenceStore{
		EvidenceDir:  evidenceDir,
		RunID:        runID,
		InvocationID: invocationID,
		safeRunID:    hex.EncodeToString(sum[:]),
	}
}

// InvocationDir returns the directory for this specific invocation's evidence.
func (s *ManagedEvidenceStore) InvocationDir() string {
	return filepath.Join(s.EvidenceDir, s.safeRunID, s.InvocationID)
}

// StageDir returns the stage-specific evidence directory.
func (s *ManagedEvidenceStore) StageDir(stage string) string {
	return filepath.Join(s.InvocationDir(), stage)
}

// WriteStageFiles writes evidence files for a stage.
// Returns a list of paths relative to the evidence directory.
func (s *ManagedEvidenceStore) WriteStageFiles(stage string, files map[string][]byte) ([]string, error) {
	stageDir := s.StageDir(stage)
	if err := os.MkdirAll(stageDir, 0o750); err != nil {
		return nil, fmt.Errorf("evidence: create stage dir %q: %w", stageDir, err)
	}
	var refs []string
	for name, data := range files {
		destPath := filepath.Join(stageDir, name)
		// Use a unique tmp name to avoid races with concurrent invocations.
		tmpPath := destPath + "." + s.InvocationID + ".tmp"
		if err := os.WriteFile(tmpPath, data, 0o600); err != nil {
			return nil, fmt.Errorf("evidence: write %q: %w", destPath, err)
		}
		if err := os.Rename(tmpPath, destPath); err != nil {
			_ = os.Remove(tmpPath)
			return nil, fmt.Errorf("evidence: rename %q: %w", destPath, err)
		}
		refs = append(refs, filepath.ToSlash(filepath.Join(s.InvocationID, stage, name)))
	}
	return refs, nil
}

// WriteManifest writes manifest.json for this invocation.
func (s *ManagedEvidenceStore) WriteManifest(manifest any) error {
	return s.writeJSON(filepath.Join(s.InvocationDir(), "manifest.json"), manifest)
}

// WriteTerminal writes terminal.json for this invocation.
func (s *ManagedEvidenceStore) WriteTerminal(terminal any) error {
	return s.writeJSON(filepath.Join(s.InvocationDir(), "terminal.json"), terminal)
}

func (s *ManagedEvidenceStore) writeJSON(path string, v any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return fmt.Errorf("evidence: create dir for %q: %w", path, err)
	}
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("evidence: marshal %q: %w", path, err)
	}
	tmp := path + "." + s.InvocationID + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("evidence: write %q: %w", path, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("evidence: rename %q: %w", path, err)
	}
	return nil
}

// TerminalManifest is written to terminal.json at run completion.
type TerminalManifest struct {
	RunID            string                   `json:"run_id"`
	MissionID        string                   `json:"mission_id"`
	InvocationID     string                   `json:"invocation_id"`
	BaseSHA          string                   `json:"base_sha"`
	InputSHA         string                   `json:"input_sha"`
	PolicyHash       string                   `json:"policy_hash"`
	StageResults     []StageResult            `json:"stage_results"`
	Findings         []FindingReportedPayload `json:"findings"`
	DecisionsApplied []string                 `json:"decisions_applied"`
	Outcome          Outcome                  `json:"outcome"`
	EventCount       int                      `json:"event_count"`
	EvidenceRefs     []string                 `json:"evidence_refs"`
	MadeVersion      string                   `json:"made_version"`
}
