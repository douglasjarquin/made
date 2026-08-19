package managed

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// ManagedEvidenceStore writes evidence files to <evidence-dir>/<run-id>/<stage>/.
// It implements evidence.Store and evidence.ContextStore indirectly via WriteStageFiles.
type ManagedEvidenceStore struct {
	EvidenceDir string
	RunID       string
}

// StageDir returns the directory for stage evidence.
func (s *ManagedEvidenceStore) StageDir(stage string) string {
	return filepath.Join(s.EvidenceDir, s.RunID, stage)
}

// WriteStageFiles writes files for a stage atomically where practical.
// Returns a list of relative paths written (relative to evidence-dir/run-id/).
func (s *ManagedEvidenceStore) WriteStageFiles(stage string, files map[string][]byte) ([]string, error) {
	stageDir := s.StageDir(stage)
	if err := os.MkdirAll(stageDir, 0o755); err != nil {
		return nil, fmt.Errorf("evidence: create stage dir %q: %w", stageDir, err)
	}
	var refs []string
	for name, data := range files {
		destPath := filepath.Join(stageDir, name)
		tmpPath := destPath + ".tmp"
		if err := os.WriteFile(tmpPath, data, 0o644); err != nil {
			return nil, fmt.Errorf("evidence: write %q: %w", destPath, err)
		}
		if err := os.Rename(tmpPath, destPath); err != nil {
			_ = os.Remove(tmpPath)
			return nil, fmt.Errorf("evidence: rename %q: %w", destPath, err)
		}
		refs = append(refs, filepath.ToSlash(filepath.Join(s.RunID, stage, name)))
	}
	return refs, nil
}

// WriteManifest writes the top-level manifest.json.
func (s *ManagedEvidenceStore) WriteManifest(manifest any) error {
	runDir := filepath.Join(s.EvidenceDir, s.RunID)
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		return fmt.Errorf("evidence: create run dir: %w", err)
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("evidence: marshal manifest: %w", err)
	}
	path := filepath.Join(runDir, "manifest.json")
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("evidence: write manifest: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("evidence: rename manifest: %w", err)
	}
	return nil
}

// WriteTerminal writes terminal.json.
func (s *ManagedEvidenceStore) WriteTerminal(terminal any) error {
	runDir := filepath.Join(s.EvidenceDir, s.RunID)
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		return fmt.Errorf("evidence: create run dir: %w", err)
	}
	data, err := json.MarshalIndent(terminal, "", "  ")
	if err != nil {
		return fmt.Errorf("evidence: marshal terminal: %w", err)
	}
	path := filepath.Join(runDir, "terminal.json")
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("evidence: write terminal: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("evidence: rename terminal: %w", err)
	}
	return nil
}

// TerminalManifest is written to terminal.json at run completion.
type TerminalManifest struct {
	RunID            string                   `json:"run_id"`
	MissionID        string                   `json:"mission_id"`
	BaseSHA          string                   `json:"base_sha"`
	InputSHA         string                   `json:"input_sha"`
	PolicyHash       string                   `json:"policy_hash"`
	StageResults     []StageResult            `json:"stage_results"`
	Findings         []FindingReportedPayload `json:"findings"`
	DecisionsApplied []string                 `json:"decisions_applied"`
	Outcome          Outcome                  `json:"outcome"`
	EventCount       int                      `json:"event_count"`
	EvidenceRefs     []string                 `json:"evidence_refs"`
}
