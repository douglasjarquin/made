package verify

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/douglasjarquin/made/internal/agent"
	"github.com/douglasjarquin/made/internal/managed"
)

const ReceiptSchemaVersion = 3

type StageReceipt struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
	// Reused lists the Test stage's lane Full commands an existing receipt
	// satisfied instead of actually running (project issue #61); nil for
	// every other stage and for a Test stage with nothing reused.
	Reused []managed.ReusedLaneCommand `json:"reused,omitempty"`
	// AgentResolution mirrors managed.StageResult.AgentResolution (Review
	// stage only, auto/empty Agent only; project: agent auto-resolve).
	AgentResolution *agent.AgentResolution `json:"agent_resolution,omitempty"`
}

type ReviewReceipt struct {
	Source         string                 `json:"source"`
	Executor       string                 `json:"executor,omitempty"`
	Reviewer       string                 `json:"reviewer,omitempty"`
	RequestedModel string                 `json:"requested_model,omitempty"`
	ActualModel    string                 `json:"actual_model,omitempty"`
	ContractHash   string                 `json:"contract_hash,omitempty"`
	Guides         []managed.GuideBinding `json:"guides,omitempty"`
}

type Receipt struct {
	SchemaVersion   int             `json:"schema_version"`
	MadeVersion     string          `json:"made_version"`
	ProtocolVersion int             `json:"protocol_version"`
	Outcome         managed.Outcome `json:"outcome"`
	Repository      string          `json:"repository"`
	BaseSHA         string          `json:"base_sha"`
	InputSHA        string          `json:"input_sha"`
	Config          ConfigIdentity  `json:"config"`
	Review          *ReviewReceipt  `json:"review,omitempty"`
	Stages          []StageReceipt  `json:"stages"`
	EvidenceRefs    []string        `json:"evidence_refs"`
	ReceiptPath     string          `json:"receipt_path"`
	EvidenceDir     string          `json:"evidence_dir"`
	Message         string          `json:"message,omitempty"`
	CreatedAt       time.Time       `json:"created_at"`
}

func BuildReceipt(repository, baseSHA, inputSHA string, cfg ConfigIdentity, review *ReviewReceipt, res EngineResult) Receipt {
	stages := make([]StageReceipt, 0, len(res.StageResults))
	for _, s := range res.StageResults {
		stages = append(stages, StageReceipt{Name: s.Stage, Status: string(s.Outcome), Message: s.Message, Reused: s.ReusedCommands, AgentResolution: s.AgentResolution})
	}
	return Receipt{
		SchemaVersion:   ReceiptSchemaVersion,
		MadeVersion:     managed.MadeVersion,
		ProtocolVersion: managed.ProtocolVersion,
		Outcome:         res.Outcome,
		Repository:      repository,
		BaseSHA:         baseSHA,
		InputSHA:        inputSHA,
		Config:          cfg,
		Review:          review,
		Stages:          stages,
		EvidenceRefs:    res.EvidenceRefs,
		ReceiptPath:     ReceiptStore{Dir: ReceiptsDir(StateRoot(repository))}.path(inputSHA),
		EvidenceDir:     res.EvidenceDir,
		Message:         res.Message,
		CreatedAt:       time.Now().UTC(),
	}
}

func reviewReceiptFromResult(res EngineResult, source, executor, reviewer, requestedModel, actualModel, contractHash string) *ReviewReceipt {
	reviewRan := false
	for _, s := range res.StageResults {
		if s.Stage == "review" && s.Outcome != managed.OutcomeNotConfigured {
			reviewRan = true
		}
	}
	if !reviewRan && len(res.Guides) == 0 {
		return nil
	}
	return &ReviewReceipt{
		Source:         source,
		Executor:       executor,
		Reviewer:       reviewer,
		RequestedModel: requestedModel,
		ActualModel:    actualModel,
		ContractHash:   contractHash,
		Guides:         res.Guides,
	}
}

type ReceiptStore struct {
	Dir string
}

func (s ReceiptStore) path(inputSHA string) string {
	return filepath.Join(s.Dir, inputSHA+".json")
}

func (s ReceiptStore) Put(r Receipt) error {
	if err := os.MkdirAll(s.Dir, 0o750); err != nil {
		return fmt.Errorf("verify: create receipts directory: %w", err)
	}
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return fmt.Errorf("verify: encode receipt: %w", err)
	}
	if err := writeFileAtomic(s.path(r.InputSHA), data, 0o600); err != nil {
		return fmt.Errorf("verify: publish receipt: %w", err)
	}
	return nil
}

func (s ReceiptStore) Get(inputSHA string) (Receipt, bool, error) {
	data, err := os.ReadFile(s.path(inputSHA))
	if err != nil {
		if os.IsNotExist(err) {
			return Receipt{}, false, nil
		}
		return Receipt{}, false, fmt.Errorf("verify: read receipt: %w", err)
	}
	var r Receipt
	if err := json.Unmarshal(data, &r); err != nil {
		return Receipt{}, false, fmt.Errorf("verify: parse receipt: %w", err)
	}
	if r.SchemaVersion != ReceiptSchemaVersion {
		return Receipt{}, false, fmt.Errorf("verify: unsupported receipt schema_version %d", r.SchemaVersion)
	}
	return r, true, nil
}

func (s ReceiptStore) List() ([]Receipt, error) {
	entries, err := os.ReadDir(s.Dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("verify: list receipts: %w", err)
	}
	var out []Receipt
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(s.Dir, e.Name()))
		if err != nil {
			continue
		}
		var r Receipt
		if err := json.Unmarshal(data, &r); err != nil || r.SchemaVersion != ReceiptSchemaVersion {
			continue
		}
		out = append(out, r)
	}
	return out, nil
}
