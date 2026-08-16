package daemon

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const runStoreRecordVersion = 1

// RunFinding is the durable Made-owned representation of a review finding.
// It deliberately contains only data needed by the public run contract.
type RunFinding struct {
	Stage      string   `json:"stage"`
	Kind       string   `json:"kind"`
	Message    string   `json:"message"`
	Paths      []string `json:"paths,omitempty"`
	PreFixSHA  string   `json:"pre_fix_sha,omitempty"`
	PostFixSHA string   `json:"post_fix_sha,omitempty"`
}

type SubmissionEvent struct {
	Gate       string    `json:"gate"`
	Ref        string    `json:"ref"`
	InputSHA   string    `json:"input_sha"`
	OutputSHA  string    `json:"output_sha,omitempty"`
	Kind       string    `json:"kind"`
	RecordedAt time.Time `json:"recorded_at"`
}

type persistedSnapshot struct {
	ID                string            `json:"run_id"`
	Repo              string            `json:"repo"`
	Branch            string            `json:"branch"`
	InputSHA          string            `json:"input_sha"`
	OutputSHA         string            `json:"output_sha"`
	Status            RunStatus         `json:"state"`
	QueuedAt          time.Time         `json:"queued_at"`
	StartedAt         time.Time         `json:"started_at"`
	EndedAt           time.Time         `json:"ended_at"`
	ExecutionFinished bool              `json:"execution_finished"`
	Message           string            `json:"message,omitempty"`
	Errors            []string          `json:"errors"`
	Findings          []RunFinding      `json:"findings"`
	Decisions         map[string]string `json:"decisions"`
	PRURL             string            `json:"pr_url"`
	SupersededBy      string            `json:"superseded_by"`
	CancelRequested   bool              `json:"cancel_requested"`
	SubmissionEvents  []SubmissionEvent `json:"submission_events"`
	Stages            []StageResult     `json:"stages"`
	PendingFindings   []AskUserFinding  `json:"pending_findings"`
	Finalized         bool              `json:"finalized"`
}

type storeRecord struct {
	Version  int               `json:"version"`
	Kind     string            `json:"kind"`
	Snapshot persistedSnapshot `json:"snapshot"`
}

// RunStore is an append-only JSON WAL. Each state transition is a complete
// snapshot, so replay needs no ordering assumptions beyond file order.
// Every append is synced before it is acknowledged to the caller.
type RunStore struct {
	path string
	mu   sync.Mutex
}

func OpenRunStore(path string) (*RunStore, map[string]RunSnapshot, error) {
	if path == "" {
		return nil, nil, errors.New("daemon: run store path is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, nil, fmt.Errorf("daemon: create run store directory: %w", err)
	}
	store := &RunStore{path: path}
	snapshots := make(map[string]RunSnapshot)
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return store, snapshots, nil
	}
	if err != nil {
		return nil, nil, fmt.Errorf("daemon: open run store: %w", err)
	}
	defer func() { _ = file.Close() }()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		var record storeRecord
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			return nil, nil, fmt.Errorf("daemon: decode run store record: %w", err)
		}
		if record.Version != runStoreRecordVersion || record.Kind != "snapshot" {
			return nil, nil, fmt.Errorf("daemon: unsupported run store record version %d or kind %q", record.Version, record.Kind)
		}
		snapshots[record.Snapshot.ID] = restoreSnapshot(record.Snapshot)
	}
	if err := scanner.Err(); err != nil {
		return nil, nil, fmt.Errorf("daemon: read run store: %w", err)
	}
	return store, snapshots, nil
}

func (s *RunStore) Append(snapshot RunSnapshot) error {
	if s == nil {
		return errors.New("daemon: nil run store")
	}
	record := storeRecord{Version: runStoreRecordVersion, Kind: "snapshot", Snapshot: persistSnapshot(snapshot)}
	data, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("daemon: encode run store record: %w", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	file, err := os.OpenFile(s.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("daemon: open run store for append: %w", err)
	}
	defer func() { _ = file.Close() }()
	if _, err := file.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("daemon: append run store: %w", err)
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf("daemon: sync run store: %w", err)
	}
	return nil
}

func persistSnapshot(snapshot RunSnapshot) persistedSnapshot {
	errorsList := append([]string(nil), snapshot.Errors...)
	if snapshot.Err != nil && len(errorsList) == 0 {
		errorsList = []string{snapshot.Err.Error()}
	}
	decisions := make(map[string]string, len(snapshot.Decisions))
	for key, value := range snapshot.Decisions {
		decisions[key] = value
	}
	return persistedSnapshot{
		ID: snapshot.ID, Repo: snapshot.Repo, Branch: snapshot.Branch,
		InputSHA: snapshot.InputSHA, OutputSHA: snapshot.OutputSHA,
		Status: snapshot.Status, QueuedAt: snapshot.QueuedAt, StartedAt: snapshot.StartedAt,
		EndedAt: snapshot.EndedAt, ExecutionFinished: snapshot.ExecutionFinished,
		Message: snapshot.Message, Errors: errorsList,
		Findings: append([]RunFinding(nil), snapshot.Findings...), Decisions: decisions,
		PRURL: snapshot.PRURL, SupersededBy: snapshot.SupersededBy,
		CancelRequested:  snapshot.CancelRequested,
		SubmissionEvents: append([]SubmissionEvent(nil), snapshot.SubmissionEvents...),
		Stages:           append([]StageResult(nil), snapshot.Stages...),
		PendingFindings:  append([]AskUserFinding(nil), snapshot.PendingFindings...),
		Finalized:        snapshot.finalized,
	}
}

func restoreSnapshot(snapshot persistedSnapshot) RunSnapshot {
	var runErr error
	if len(snapshot.Errors) > 0 {
		runErr = errors.New(snapshot.Errors[len(snapshot.Errors)-1])
	}
	return RunSnapshot{
		ID: snapshot.ID, Repo: snapshot.Repo, Branch: snapshot.Branch,
		InputSHA: snapshot.InputSHA, OutputSHA: snapshot.OutputSHA,
		Status: snapshot.Status, QueuedAt: snapshot.QueuedAt, StartedAt: snapshot.StartedAt,
		EndedAt: snapshot.EndedAt, ExecutionFinished: snapshot.ExecutionFinished,
		Err: runErr, Errors: append([]string(nil), snapshot.Errors...),
		Message: snapshot.Message, Findings: append([]RunFinding(nil), snapshot.Findings...),
		Decisions: snapshot.Decisions, PRURL: snapshot.PRURL,
		SupersededBy: snapshot.SupersededBy, CancelRequested: snapshot.CancelRequested,
		SubmissionEvents: append([]SubmissionEvent(nil), snapshot.SubmissionEvents...),
		Stages:           append([]StageResult(nil), snapshot.Stages...),
		PendingFindings:  append([]AskUserFinding(nil), snapshot.PendingFindings...),
		finalized:        snapshot.Finalized,
	}
}
