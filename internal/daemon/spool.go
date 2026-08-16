package daemon

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

type GateSubmission struct {
	Gate  string `json:"gate"`
	Ref   string `json:"ref"`
	SHA   string `json:"sha"`
	RunID string `json:"run_id"`
}

type spoolRecord struct {
	Kind       string         `json:"kind"`
	Submission GateSubmission `json:"submission"`
}

type GateSpool struct {
	path    string
	mu      sync.Mutex
	pending map[string]GateSubmission
	seen    map[string]GateSubmission
}

func OpenGateSpool(path string) (*GateSpool, error) {
	if path == "" {
		return nil, errors.New("daemon: gate spool path is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("daemon: create gate spool directory: %w", err)
	}
	spool := &GateSpool{path: path, pending: make(map[string]GateSubmission), seen: make(map[string]GateSubmission)}
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return spool, nil
	}
	if err != nil {
		return nil, fmt.Errorf("daemon: open gate spool: %w", err)
	}
	defer func() { _ = file.Close() }()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var record spoolRecord
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			return nil, fmt.Errorf("daemon: decode gate spool: %w", err)
		}
		key := gateSubmissionKey(record.Submission)
		switch record.Kind {
		case "enqueue":
			spool.pending[key] = record.Submission
			spool.seen[key] = record.Submission
		case "drain":
			delete(spool.pending, key)
		default:
			return nil, fmt.Errorf("daemon: unknown gate spool record %q", record.Kind)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("daemon: read gate spool: %w", err)
	}
	return spool, nil
}

func (s *GateSpool) Enqueue(submission GateSubmission) (GateSubmission, bool, error) {
	if submission.Gate == "" || submission.Ref == "" || submission.SHA == "" || submission.RunID == "" {
		return GateSubmission{}, false, errors.New("daemon: incomplete gate submission identity")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	key := gateSubmissionKey(submission)
	if existing, ok := s.seen[key]; ok {
		return existing, false, nil
	}
	if err := s.appendLocked(spoolRecord{Kind: "enqueue", Submission: submission}); err != nil {
		return GateSubmission{}, false, err
	}
	s.pending[key] = submission
	s.seen[key] = submission
	return submission, true, nil
}

func (s *GateSpool) Drain(submission GateSubmission) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := gateSubmissionKey(submission)
	if _, ok := s.pending[key]; !ok {
		return nil
	}
	if err := s.appendLocked(spoolRecord{Kind: "drain", Submission: submission}); err != nil {
		return err
	}
	delete(s.pending, key)
	return nil
}

func (s *GateSpool) HasPending() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.pending) > 0
}

func (s *GateSpool) appendLocked(record spoolRecord) error {
	data, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("daemon: encode gate spool record: %w", err)
	}
	file, err := os.OpenFile(s.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("daemon: open gate spool for append: %w", err)
	}
	defer func() { _ = file.Close() }()
	if _, err := file.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("daemon: append gate spool: %w", err)
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf("daemon: sync gate spool: %w", err)
	}
	return nil
}

func gateSubmissionKey(submission GateSubmission) string {
	return submission.Gate + "\x00" + submission.Ref + "\x00" + submission.SHA
}
