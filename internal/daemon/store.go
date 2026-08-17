package daemon

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/douglasjarquin/made/internal/evidence"
	"golang.org/x/sys/unix"
)

const runStoreRecordVersion = 1
const maxRunStoreRecordBytes = 4 << 20

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
	file, err := os.OpenFile(path, os.O_RDONLY|unix.O_NOFOLLOW, 0)
	if errors.Is(err, os.ErrNotExist) {
		return store, snapshots, nil
	}
	if err != nil {
		return nil, nil, fmt.Errorf("daemon: open run store: %w", err)
	}
	defer func() { _ = file.Close() }()

	reader := bufio.NewReader(file)
	for {
		line, readErr := readRecordLine(reader, maxRunStoreRecordBytes)
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return nil, nil, fmt.Errorf("daemon: read run store: %w", readErr)
		}
		if len(line) == 0 {
			continue
		}
		var record storeRecord
		if err := json.Unmarshal(line, &record); err != nil {
			return nil, nil, fmt.Errorf("daemon: decode run store record: %w", err)
		}
		if record.Version != runStoreRecordVersion || record.Kind != "snapshot" {
			return nil, nil, fmt.Errorf("daemon: unsupported run store record version %d or kind %q", record.Version, record.Kind)
		}
		snapshots[record.Snapshot.ID] = restoreSnapshot(record.Snapshot)
	}
	return store, snapshots, nil
}

func readRecordLine(reader *bufio.Reader, maxBytes int) ([]byte, error) {
	maxLineBytes := maxBytes + 1
	var line []byte
	for {
		chunk, err := reader.ReadSlice('\n')
		line = append(line, chunk...)
		if len(line) > maxLineBytes {
			return nil, fmt.Errorf("record exceeds %d bytes", maxBytes)
		}
		if err == bufio.ErrBufferFull {
			continue
		}
		if err == io.EOF {
			if len(line) > maxBytes {
				return nil, fmt.Errorf("record exceeds %d bytes", maxBytes)
			}
			return nil, io.EOF
		}
		if err != nil {
			return nil, err
		}
		return line[:len(line)-1], nil
	}
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
	if len(data) > maxRunStoreRecordBytes {
		return fmt.Errorf("daemon: run store record exceeds %d bytes", maxRunStoreRecordBytes)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	file, err := os.OpenFile(s.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND|unix.O_NOFOLLOW, 0o600)
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
	for i := range errorsList {
		errorsList[i] = evidence.RedactString(errorsList[i])
	}
	decisions := make(map[string]string, len(snapshot.Decisions))
	for key, value := range snapshot.Decisions {
		decisions[evidence.RedactString(key)] = evidence.RedactString(value)
	}
	findings := redactFindings(snapshot.Findings)
	pendingFindings := redactPendingFindings(snapshot.PendingFindings)
	return persistedSnapshot{
		ID: snapshot.ID, Repo: snapshot.Repo, Branch: snapshot.Branch,
		InputSHA: snapshot.InputSHA, OutputSHA: snapshot.OutputSHA,
		Status: snapshot.Status, QueuedAt: snapshot.QueuedAt, StartedAt: snapshot.StartedAt,
		EndedAt: snapshot.EndedAt, ExecutionFinished: snapshot.ExecutionFinished,
		Message: evidence.RedactString(snapshot.Message), Errors: errorsList,
		Findings: findings, Decisions: decisions,
		PRURL: evidence.RedactString(snapshot.PRURL), SupersededBy: evidence.RedactString(snapshot.SupersededBy),
		CancelRequested:  snapshot.CancelRequested,
		SubmissionEvents: redactSubmissionEvents(snapshot.SubmissionEvents),
		Stages:           append([]StageResult(nil), snapshot.Stages...),
		PendingFindings:  pendingFindings,
		Finalized:        snapshot.finalized,
	}
}

func restoreSnapshot(snapshot persistedSnapshot) RunSnapshot {
	var runErr error
	if len(snapshot.Errors) > 0 {
		runErr = errors.New(evidence.RedactString(snapshot.Errors[len(snapshot.Errors)-1]))
	}
	return RunSnapshot{
		ID: snapshot.ID, Repo: snapshot.Repo, Branch: snapshot.Branch,
		InputSHA: snapshot.InputSHA, OutputSHA: snapshot.OutputSHA,
		Status: snapshot.Status, QueuedAt: snapshot.QueuedAt, StartedAt: snapshot.StartedAt,
		EndedAt: snapshot.EndedAt, ExecutionFinished: snapshot.ExecutionFinished,
		Err: runErr, Errors: redactStrings(snapshot.Errors),
		Message: evidence.RedactString(snapshot.Message), Findings: redactFindings(snapshot.Findings),
		Decisions: snapshot.Decisions, PRURL: snapshot.PRURL,
		SupersededBy: snapshot.SupersededBy, CancelRequested: snapshot.CancelRequested,
		SubmissionEvents: redactSubmissionEvents(snapshot.SubmissionEvents),
		Stages:           append([]StageResult(nil), snapshot.Stages...),
		PendingFindings:  redactPendingFindings(snapshot.PendingFindings),
		finalized:        snapshot.Finalized,
	}
}

func redactStrings(values []string) []string {
	if values == nil {
		return nil
	}
	redacted := make([]string, len(values))
	for i, value := range values {
		redacted[i] = evidence.RedactString(value)
	}
	return redacted
}

func redactFindings(values []RunFinding) []RunFinding {
	if values == nil {
		return nil
	}
	redacted := make([]RunFinding, len(values))
	for i, value := range values {
		value.Message = evidence.RedactString(value.Message)
		for j, path := range value.Paths {
			value.Paths[j] = evidence.RedactString(path)
		}
		redacted[i] = value
	}
	return redacted
}

func redactPendingFindings(values []AskUserFinding) []AskUserFinding {
	if values == nil {
		return nil
	}
	redacted := make([]AskUserFinding, len(values))
	for i, value := range values {
		value.Stage = evidence.RedactString(value.Stage)
		value.Message = evidence.RedactString(value.Message)
		redacted[i] = value
	}
	return redacted
}

func redactSubmissionEvents(values []SubmissionEvent) []SubmissionEvent {
	if values == nil {
		return nil
	}
	redacted := make([]SubmissionEvent, len(values))
	for i, value := range values {
		value.Gate = evidence.RedactString(value.Gate)
		value.Ref = evidence.RedactString(value.Ref)
		value.InputSHA = evidence.RedactString(value.InputSHA)
		value.OutputSHA = evidence.RedactString(value.OutputSHA)
		value.Kind = evidence.RedactString(value.Kind)
		redacted[i] = value
	}
	return redacted
}
