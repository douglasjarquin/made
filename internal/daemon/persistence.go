package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	walFileName      = "runs.wal"
	snapshotFileName = "runs.snapshot.json"
	maxWALBytes      = 1 << 20
	maxWALRecords    = 512
)

// RunSubmission is the immutable identity supplied by one accepted git push.
// It is persisted before the job enters the in-memory queue so a restart can
// distinguish a refresh of the same submission from an unrelated run.
type RunSubmission struct {
	ID           string `json:"run_id,omitempty"`
	Repo         string `json:"repo"`
	Branch       string `json:"branch"`
	Ref          string `json:"ref,omitempty"`
	OldSHA       string `json:"old_sha,omitempty"`
	InputSHA     string `json:"input_sha,omitempty"`
	OutputSHA    string `json:"output_sha,omitempty"`
	SubmissionID string `json:"submission_id,omitempty"`
	GatePath     string `json:"gate_path,omitempty"`
}

func (s RunSubmission) snapshot(queuedAt time.Time) RunSnapshot {
	return RunSnapshot{
		ID:                s.ID,
		Repo:              s.Repo,
		Branch:            s.Branch,
		Ref:               s.Ref,
		OldSHA:            s.OldSHA,
		InputSHA:          s.InputSHA,
		OutputSHA:         s.OutputSHA,
		SubmissionID:      s.SubmissionID,
		GatePath:          s.GatePath,
		Status:            RunQueued,
		QueuedAt:          queuedAt,
		Stages:            []StageResult{},
		PendingFindings:   []AskUserFinding{},
		EvidenceRefs:      []string{},
		Decisions:         map[string]string{},
		ExecutionFinished: false,
	}
}

type walRecord struct {
	Snapshot RunSnapshot `json:"snapshot"`
}

type checkpoint struct {
	Counter uint64        `json:"counter"`
	Runs    []RunSnapshot `json:"runs"`
}

type runStore struct {
	dir          string
	walPath      string
	snapshotPath string

	mu      sync.Mutex
	records int
	closed  bool
}

func openRunStore(dir string) (*runStore, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("daemon: create state directory: %w", err)
	}
	return &runStore{
		dir:          dir,
		walPath:      filepath.Join(dir, walFileName),
		snapshotPath: filepath.Join(dir, snapshotFileName),
	}, nil
}

func (s *runStore) load() ([]RunSnapshot, uint64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var state checkpoint
	data, err := os.ReadFile(s.snapshotPath)
	if err == nil {
		if err := json.Unmarshal(data, &state); err != nil {
			return nil, 0, fmt.Errorf("daemon: decode run checkpoint: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, 0, fmt.Errorf("daemon: read run checkpoint: %w", err)
	}

	byID := make(map[string]RunSnapshot, len(state.Runs))
	for _, snap := range state.Runs {
		byID[snap.ID] = restoreSnapshot(snap)
	}

	wal, err := os.ReadFile(s.walPath)
	if err == nil {
		lines := bytes.Split(wal, []byte{'\n'})
		for i, line := range lines {
			if len(bytes.TrimSpace(line)) == 0 {
				continue
			}
			var record walRecord
			if err := json.Unmarshal(line, &record); err != nil {
				if i == len(lines)-1 {
					// A torn final append is safe to ignore because every
					// record before it was fsynced before it became visible.
					break
				}
				return nil, 0, fmt.Errorf("daemon: decode run WAL record %d: %w", i, err)
			}
			if record.Snapshot.ID == "" {
				return nil, 0, fmt.Errorf("daemon: run WAL record %d has empty run ID", i)
			}
			byID[record.Snapshot.ID] = restoreSnapshot(record.Snapshot)
			s.records++
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, 0, fmt.Errorf("daemon: read run WAL: %w", err)
	}

	runs := make([]RunSnapshot, 0, len(byID))
	var maxID uint64
	for _, snap := range byID {
		runs = append(runs, snap)
		if n, ok := runIDNumber(snap.ID); ok && n > maxID {
			maxID = n
		}
	}
	if state.Counter > maxID {
		maxID = state.Counter
	}
	return runs, maxID, nil
}

func (s *runStore) append(snapshot RunSnapshot) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errors.New("daemon: run store is closed")
	}

	data, err := json.Marshal(walRecord{Snapshot: snapshotForStorage(snapshot)})
	if err != nil {
		return fmt.Errorf("daemon: encode run WAL record: %w", err)
	}
	file, err := os.OpenFile(s.walPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("daemon: open run WAL: %w", err)
	}
	if _, err := file.Write(append(data, '\n')); err != nil {
		_ = file.Close()
		return fmt.Errorf("daemon: append run WAL: %w", err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return fmt.Errorf("daemon: sync run WAL: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("daemon: close run WAL: %w", err)
	}
	s.records++
	return nil
}

func (s *runStore) shouldCompact() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.records >= maxWALRecords || fileSize(s.walPath) >= maxWALBytes
}

func (s *runStore) compact(runs []RunSnapshot, counter uint64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errors.New("daemon: run store is closed")
	}

	data, err := json.MarshalIndent(checkpoint{Counter: counter, Runs: snapshotsForStorage(runs)}, "", "  ")
	if err != nil {
		return fmt.Errorf("daemon: encode run checkpoint: %w", err)
	}
	tmp, err := os.CreateTemp(s.dir, ".runs.snapshot-*")
	if err != nil {
		return fmt.Errorf("daemon: create run checkpoint: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("daemon: chmod run checkpoint: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("daemon: write run checkpoint: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("daemon: sync run checkpoint: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("daemon: close run checkpoint: %w", err)
	}
	if err := os.Rename(tmpName, s.snapshotPath); err != nil {
		return fmt.Errorf("daemon: install run checkpoint: %w", err)
	}
	dirFile, err := os.Open(s.dir)
	if err != nil {
		return fmt.Errorf("daemon: open state directory: %w", err)
	}
	if err := dirFile.Sync(); err != nil {
		_ = dirFile.Close()
		return fmt.Errorf("daemon: sync state directory: %w", err)
	}
	if err := dirFile.Close(); err != nil {
		return fmt.Errorf("daemon: close state directory: %w", err)
	}
	if err := os.WriteFile(s.walPath, nil, 0o600); err != nil {
		return fmt.Errorf("daemon: truncate run WAL: %w", err)
	}
	s.records = 0
	return nil
}

func (s *runStore) close(runs []RunSnapshot, counter uint64) error {
	if err := s.compact(runs, counter); err != nil {
		return err
	}
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
	return nil
}

func fileSize(path string) int64 {
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return info.Size()
}

func snapshotForStorage(snapshot RunSnapshot) RunSnapshot {
	copy := cloneSnapshot(snapshot)
	if copy.Err != nil && copy.Error == "" {
		copy.Error = copy.Err.Error()
	}
	copy.Err = nil
	return copy
}

func snapshotsForStorage(snapshots []RunSnapshot) []RunSnapshot {
	out := make([]RunSnapshot, len(snapshots))
	for i, snapshot := range snapshots {
		out[i] = snapshotForStorage(snapshot)
	}
	return out
}

func restoreSnapshot(snapshot RunSnapshot) RunSnapshot {
	snapshot = cloneSnapshot(snapshot)
	if snapshot.Error != "" {
		snapshot.Err = errors.New(snapshot.Error)
	}
	if snapshot.Stages == nil {
		snapshot.Stages = []StageResult{}
	}
	if snapshot.PendingFindings == nil {
		snapshot.PendingFindings = []AskUserFinding{}
	}
	if snapshot.EvidenceRefs == nil {
		snapshot.EvidenceRefs = []string{}
	}
	if snapshot.Decisions == nil {
		snapshot.Decisions = map[string]string{}
	}
	return snapshot
}

func cloneSnapshot(snapshot RunSnapshot) RunSnapshot {
	copy := snapshot
	copy.Stages = append([]StageResult(nil), snapshot.Stages...)
	for i := range copy.Stages {
		copy.Stages[i].EvidenceRefs = append([]string(nil), snapshot.Stages[i].EvidenceRefs...)
	}
	copy.PendingFindings = append([]AskUserFinding(nil), snapshot.PendingFindings...)
	copy.EvidenceRefs = append([]string(nil), snapshot.EvidenceRefs...)
	if snapshot.Decisions != nil {
		copy.Decisions = make(map[string]string, len(snapshot.Decisions))
		for key, value := range snapshot.Decisions {
			copy.Decisions[key] = value
		}
	}
	return copy
}

func runIDNumber(id string) (uint64, bool) {
	value := strings.TrimPrefix(id, "run-")
	if value == id || value == "" {
		return 0, false
	}
	n, err := strconv.ParseUint(value, 10, 64)
	return n, err == nil
}

// OpenRunManager restores terminal and awaiting-merge records from the
// durable run store. In-flight work is never silently replayed without its
// original WorkFunc; the persisted submission remains queryable for an
// explicit refresh using the same submission identity.
func OpenRunManager(stateDir string) (*RunManager, error) {
	store, err := openRunStore(stateDir)
	if err != nil {
		return nil, err
	}
	rm := newRunManager(store)
	runs, counter, err := store.load()
	if err != nil {
		_ = store.close(nil, 0)
		return nil, err
	}
	atomic.StoreUint64(&rm.counter, counter)
	for _, snapshot := range runs {
		if snapshot.Status == RunRunning {
			snapshot.Status = RunFailed
			snapshot.Error = "daemon restarted before run execution finished"
			snapshot.Err = errors.New(snapshot.Error)
			snapshot.EndedAt = time.Now()
			snapshot.ExecutionFinished = true
		}
		ctx, cancel := context.WithCancel(context.Background())
		r := &run{ctx: ctx, cancel: cancel, snap: restoreSnapshot(snapshot)}
		rm.runs[snapshot.ID] = r
		if snapshot.Status == RunQueued {
			// Queue refresh is explicit: no work is replayed merely because
			// a daemon restarted.
			rm.repos[snapshot.Repo] = &repoQueue{}
		}
		if snapshot.Status == RunFailed && snapshot.Error == "daemon restarted before run execution finished" {
			rm.mu.Lock()
			err := rm.persistSnapshotLocked(snapshot)
			rm.mu.Unlock()
			if err != nil {
				cancel()
				_ = store.close(nil, 0)
				return nil, err
			}
		}
	}
	return rm, nil
}

func (rm *RunManager) Close() error {
	if rm.store == nil {
		return nil
	}
	runs := rm.List()
	return rm.store.close(runs, atomic.LoadUint64(&rm.counter))
}

func (rm *RunManager) persistSnapshotLocked(snapshot RunSnapshot) error {
	if rm.store == nil {
		return nil
	}
	if err := rm.store.append(snapshot); err != nil {
		return err
	}
	if rm.store.shouldCompact() {
		return rm.store.compact(rm.snapshotsLocked(), atomic.LoadUint64(&rm.counter))
	}
	return nil
}

func (rm *RunManager) snapshotsLocked() []RunSnapshot {
	out := make([]RunSnapshot, 0, len(rm.runs))
	for _, r := range rm.runs {
		out = append(out, r.snapshot())
	}
	return out
}

func (rm *RunManager) FindSubmission(submission RunSubmission) (RunSnapshot, bool) {
	rm.mu.Lock()
	runs := make([]*run, 0, len(rm.runs))
	for _, r := range rm.runs {
		runs = append(runs, r)
	}
	rm.mu.Unlock()
	for _, r := range runs {
		snapshot := r.snapshot()
		if submission.SubmissionID != "" && snapshot.SubmissionID == submission.SubmissionID {
			return snapshot, true
		}
		if submission.InputSHA != "" && submission.Repo != "" && snapshot.Repo == submission.Repo &&
			snapshot.Branch == submission.Branch && snapshot.Ref == submission.Ref &&
			snapshot.InputSHA == submission.InputSHA {
			return snapshot, true
		}
	}
	return RunSnapshot{}, false
}

func (rm *RunManager) UpdateDecision(id, stage, decision string) error {
	r, ok := rm.lookupRun(id)
	if !ok {
		return fmt.Errorf("daemon: no run %q", id)
	}
	r.update(func(snapshot *RunSnapshot) {
		if snapshot.Decisions == nil {
			snapshot.Decisions = make(map[string]string)
		}
		snapshot.Decisions[stage] = decision
	})
	rm.mu.Lock()
	err := rm.persistSnapshotLocked(r.snapshot())
	rm.mu.Unlock()
	return err
}
