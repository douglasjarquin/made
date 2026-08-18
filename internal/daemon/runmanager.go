package daemon

import (
	"context"
	cryptorand "crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type RunStatus string

const (
	RunQueued         RunStatus = "queued"
	RunRunning        RunStatus = "running"
	RunAwaitingReview RunStatus = "awaiting_review"
	RunAwaitingMerge  RunStatus = "awaiting_merge"
	RunSucceeded      RunStatus = "succeeded"
	RunCompleted      RunStatus = RunSucceeded
	RunFailed         RunStatus = "failed"
	RunCanceled       RunStatus = "canceled"
	RunSuperseded     RunStatus = "superseded"
)

type RunSnapshot struct {
	ID                string            `json:"run_id"`
	Repo              string            `json:"repo"`
	Branch            string            `json:"branch"`
	Ref               string            `json:"ref,omitempty"`
	OldSHA            string            `json:"old_sha,omitempty"`
	InputSHA          string            `json:"input_sha,omitempty"`
	OutputSHA         string            `json:"output_sha,omitempty"`
	SubmissionID      string            `json:"submission_id,omitempty"`
	GatePath          string            `json:"gate_path,omitempty"`
	Status            RunStatus         `json:"state"`
	QueuedAt          time.Time         `json:"queued_at"`
	StartedAt         time.Time         `json:"started_at"`
	EndedAt           time.Time         `json:"ended_at"`
	Err               error             `json:"-"`
	Error             string            `json:"error,omitempty"`
	Message           string            `json:"message,omitempty"`
	Errors            []string          `json:"errors,omitempty"`
	Findings          []RunFinding      `json:"findings,omitempty"`
	PRURL             string            `json:"pr_url,omitempty"`
	SupersededBy      string            `json:"superseded_by,omitempty"`
	CancelRequested   bool              `json:"cancel_requested,omitempty"`
	SubmissionEvents  []SubmissionEvent `json:"submission_events,omitempty"`
	Stages            []StageResult     `json:"stages"`
	PendingFindings   []AskUserFinding  `json:"pending_findings"`
	EvidenceRefs      []string          `json:"evidence_refs,omitempty"`
	CurrentStage      string            `json:"current_stage,omitempty"`
	Decisions         map[string]string `json:"decisions,omitempty"`
	ExecutionFinished bool              `json:"execution_finished"`

	// finalized is set by Finish and read by execute so a WorkFunc can declare
	// an awaiting-merge or terminal result without being overwritten when it
	// returns.
	finalized bool
}

type WorkFunc func(ctx context.Context, emit func(Event)) error

var ErrRunIDExists = errors.New("daemon: run ID already submitted")

var ErrRunSubmissionClosed = errors.New("daemon: run submission is closed")

type run struct {
	mu        sync.Mutex
	persistMu sync.Mutex
	snap      RunSnapshot
	ctx       context.Context
	cancel    context.CancelFunc
}

func (r *run) snapshot() RunSnapshot {
	r.mu.Lock()
	defer r.mu.Unlock()
	return cloneSnapshot(r.snap)
}

func (r *run) replace(snapshot RunSnapshot) {
	r.mu.Lock()
	r.snap = cloneSnapshot(snapshot)
	r.mu.Unlock()
}

type queuedJob struct {
	run  *run
	work WorkFunc
}

type repoQueue struct {
	mu      sync.Mutex
	pending []*queuedJob
	active  bool
}

// RunManager serializes runs per repo identifier via one FIFO+drain-goroutine
// pair per repo: made's bare-gate-repo model allows only one checked-out
// worktree per bare repo at a time, so a second push against a repo already
// running must queue behind it rather than run concurrently or be rejected.
type RunManager struct {
	mailbox  *Mailbox
	activity chan struct{}
	store    *runStore

	beforeCloseCompact func()
	durableMu          sync.Mutex

	mu      sync.Mutex
	repos   map[string]*repoQueue
	runs    map[string]*run
	closing bool
	counter atomic.Uint64
}

func NewRunManager() *RunManager {
	return newRunManager(nil)
}

func newRunManager(store *runStore) *RunManager {
	return &RunManager{
		mailbox:  NewMailbox(),
		activity: make(chan struct{}, 1),
		store:    store,
		repos:    make(map[string]*repoQueue),
		runs:     make(map[string]*run),
	}
}

func (rm *RunManager) ActivitySignal() <-chan struct{} {
	return rm.activity
}

func (rm *RunManager) BeginShutdown() error {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	for _, r := range rm.runs {
		snapshot := r.snapshot()
		if snapshot.Status == RunQueued || snapshot.Status == RunRunning || snapshot.Status == RunAwaitingReview || snapshot.Status == RunAwaitingMerge {
			return fmt.Errorf("daemon: active run %q remains in state %s", snapshot.ID, snapshot.Status)
		}
	}
	rm.closing = true
	return nil
}

func (rm *RunManager) StopAccepting() {
	rm.mu.Lock()
	rm.closing = true
	rm.mu.Unlock()
}

func (rm *RunManager) Accepting() bool {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	return !rm.closing
}

// Non-blocking send: a run must never wait on whether anyone is listening
// for activity, and a still-pending signal already means "reset the idle
// timer," so a full buffer can just drop this one without losing meaning.
func (rm *RunManager) signalActivity() {
	select {
	case rm.activity <- struct{}{}:
	default:
	}
}

func (rm *RunManager) NewRunID() string {
	return NewRunID()
}

var fallbackRunIDCounter atomic.Uint64

func NewRunID() string {
	var id [16]byte
	if _, err := cryptorand.Read(id[:]); err != nil {
		binary.BigEndian.PutUint64(id[:8], uint64(time.Now().UnixNano()))
		binary.BigEndian.PutUint64(id[8:], fallbackRunIDCounter.Add(1))
	}
	id[6] = (id[6] & 0x0f) | 0x40
	id[8] = (id[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		binary.BigEndian.Uint32(id[0:4]),
		binary.BigEndian.Uint16(id[4:6]),
		binary.BigEndian.Uint16(id[6:8]),
		binary.BigEndian.Uint16(id[8:10]),
		uint64(id[10])<<40|uint64(id[11])<<32|uint64(id[12])<<24|uint64(id[13])<<16|uint64(id[14])<<8|uint64(id[15]))
}

func (rm *RunManager) Submit(id, repo, branch string, work WorkFunc) (RunSnapshot, error) {
	return rm.SubmitSubmission(RunSubmission{ID: id, Repo: repo, Branch: branch}, work)
}

func (rm *RunManager) SubmitWithMetadata(id, repo, branch, inputSHA, outputSHA string, work WorkFunc) (RunSnapshot, error) {
	return rm.SubmitSubmission(RunSubmission{
		ID: id, Repo: repo, Branch: branch, InputSHA: inputSHA, OutputSHA: outputSHA,
	}, work)
}

func (rm *RunManager) SubmitSubmission(submission RunSubmission, work WorkFunc) (RunSnapshot, error) {
	if strings.TrimSpace(submission.ID) == "" {
		return RunSnapshot{}, fmt.Errorf("daemon: run ID must not be empty")
	}
	ctx, cancel := context.WithCancel(context.Background())
	r := &run{
		ctx:    ctx,
		cancel: cancel,
		snap:   submission.snapshot(time.Now()),
	}
	queuedSnapshot := cloneSnapshot(r.snap)

	rm.durableMu.Lock()
	rm.mu.Lock()
	if rm.closing {
		rm.mu.Unlock()
		rm.durableMu.Unlock()
		cancel()
		return RunSnapshot{}, ErrRunSubmissionClosed
	}
	if _, exists := rm.runs[submission.ID]; exists {
		rm.mu.Unlock()
		rm.durableMu.Unlock()
		return RunSnapshot{}, ErrRunIDExists
	}
	if err := rm.persistSnapshotLocked(r.snap); err != nil {
		rm.mu.Unlock()
		rm.durableMu.Unlock()
		cancel()
		return RunSnapshot{}, fmt.Errorf("daemon: persist submission: %w", err)
	}
	rm.runs[submission.ID] = r
	rq, ok := rm.repos[submission.Repo]
	if !ok {
		rq = &repoQueue{}
		rm.repos[submission.Repo] = rq
	}
	rm.mu.Unlock()
	rm.durableMu.Unlock()
	if work == nil {
		return queuedSnapshot, nil
	}

	rq.mu.Lock()
	rq.pending = append(rq.pending, &queuedJob{run: r, work: work})
	startDrain := !rq.active
	rq.active = true
	rq.mu.Unlock()

	if startDrain {
		go rm.drain(rq)
	}

	return queuedSnapshot, nil
}

func (rm *RunManager) RefreshQueued(id string, work WorkFunc) error {
	r, ok := rm.lookupRun(id)
	if !ok {
		return fmt.Errorf("daemon: no run %q", id)
	}
	snapshot := r.snapshot()
	if snapshot.Status != RunQueued {
		return fmt.Errorf("daemon: run %q is %s, not queued", id, snapshot.Status)
	}
	rm.mu.Lock()
	rq := rm.repos[snapshot.Repo]
	rm.mu.Unlock()
	if rq == nil {
		return fmt.Errorf("daemon: no queue for run %q", id)
	}
	rq.mu.Lock()
	for _, job := range rq.pending {
		if job.run == r {
			rq.mu.Unlock()
			return nil
		}
	}
	rq.pending = append(rq.pending, &queuedJob{run: r, work: work})
	startDrain := !rq.active
	rq.active = true
	rq.mu.Unlock()
	if startDrain {
		go rm.drain(rq)
	}
	return nil
}

func (rm *RunManager) drain(rq *repoQueue) {
	for {
		rq.mu.Lock()
		if len(rq.pending) == 0 {
			rq.active = false
			rq.mu.Unlock()
			return
		}
		j := rq.pending[0]
		rq.pending = rq.pending[1:]
		rq.mu.Unlock()

		rm.execute(j.run, j.work)
	}
}

func (rm *RunManager) execute(r *run, work WorkFunc) {
	initial := r.snapshot()
	if isTerminalRunStatus(initial.Status) {
		return
	}
	id := initial.ID
	started := time.Now()
	r.persistMu.Lock()
	startedSnapshot := r.snapshot()
	if startedSnapshot.Status != RunQueued {
		r.persistMu.Unlock()
		return
	}
	startedSnapshot.Status = RunRunning
	startedSnapshot.StartedAt = started
	if err := rm.persistAndReplace(r, startedSnapshot); err != nil {
		r.persistMu.Unlock()
		rm.failAfterPersistenceError(r, err)
		return
	}
	r.persistMu.Unlock()
	rm.mailbox.Publish(Event{RunID: id, Kind: EventRunStarted, Time: started})
	rm.signalActivity()

	emit := func(ev Event) {
		ev.RunID = id
		if ev.Time.IsZero() {
			ev.Time = time.Now()
		}
		rm.mailbox.Publish(ev)
		rm.signalActivity()
	}

	var err error
	if work == nil {
		err = errors.New("daemon: nil run work function")
	} else {
		err = work(r.ctx, emit)
	}
	rm.signalActivity()

	ended := time.Now()
	r.persistMu.Lock()
	finishedSnapshot := r.snapshot()
	finishedSnapshot.EndedAt = ended
	finishedSnapshot.ExecutionFinished = true
	if !finishedSnapshot.finalized {
		finishedSnapshot.Err = err
		finishedSnapshot.Error = ""
		if err != nil {
			finishedSnapshot.Error = err.Error()
			if errors.Is(err, context.Canceled) {
				finishedSnapshot.Status = RunCanceled
			} else {
				finishedSnapshot.Status = RunFailed
			}
		} else {
			finishedSnapshot.Status = RunSucceeded
		}
	}
	if err := rm.persistAndReplace(r, finishedSnapshot); err != nil {
		r.persistMu.Unlock()
		rm.failAfterPersistenceError(r, err)
		return
	}
	r.persistMu.Unlock()

	snapshot := r.snapshot()
	var finalKind EventKind
	switch snapshot.Status {
	case RunSucceeded:
		finalKind = EventRunCompleted
	case RunFailed:
		finalKind = EventRunFailed
	case RunCanceled, RunSuperseded:
		finalKind = EventRunCanceled
	default:
		return
	}
	rm.mailbox.Publish(Event{RunID: id, Kind: finalKind, Time: ended, Err: err})
}

func (rm *RunManager) Snapshot(id string) (RunSnapshot, bool) {
	r, ok := rm.lookupRun(id)
	if !ok {
		return RunSnapshot{}, false
	}
	return r.snapshot(), true
}

func (rm *RunManager) List() []RunSnapshot {
	rm.mu.Lock()
	runs := make([]*run, 0, len(rm.runs))
	for _, r := range rm.runs {
		runs = append(runs, r)
	}
	rm.mu.Unlock()

	snaps := make([]RunSnapshot, len(runs))
	for i, r := range runs {
		snaps[i] = r.snapshot()
	}
	sort.Slice(snaps, func(i, j int) bool {
		if snaps[i].QueuedAt.Equal(snaps[j].QueuedAt) {
			return snaps[i].ID < snaps[j].ID
		}
		return snaps[i].QueuedAt.Before(snaps[j].QueuedAt)
	})
	return snaps
}

func (rm *RunManager) Subscribe(id string) (<-chan Event, func()) {
	return rm.mailbox.Subscribe(id)
}

func (rm *RunManager) Cancel(id string) error {
	r, ok := rm.lookupRun(id)
	if !ok {
		return fmt.Errorf("daemon: no run %q", id)
	}
	snapshot := r.snapshot()
	if isTerminalRunStatus(snapshot.Status) {
		return fmt.Errorf("daemon: run %q is already %s", id, snapshot.Status)
	}
	if snapshot.Status == RunQueued {
		if handled, err := rm.cancelQueued(r); handled {
			return err
		}
	}
	r.cancel()
	return nil
}

func isTerminalRunStatus(s RunStatus) bool {
	return s == RunSucceeded || s == RunFailed || s == RunCanceled || s == RunSuperseded
}

func (rm *RunManager) Finish(id string, status RunStatus, message string) error {
	r, ok := rm.lookupRun(id)
	if !ok {
		return fmt.Errorf("daemon: no run %q", id)
	}
	r.persistMu.Lock()
	defer r.persistMu.Unlock()
	candidate := r.snapshot()
	candidate.Status = status
	candidate.Message = message
	candidate.ExecutionFinished = status == RunAwaitingMerge || isTerminalRunStatus(status)
	candidate.finalized = true
	if err := rm.persistAndReplace(r, candidate); err != nil {
		return err
	}
	return nil
}

func (rm *RunManager) failAfterPersistenceError(r *run, persistErr error) {
	r.persistMu.Lock()
	defer r.persistMu.Unlock()
	failure := fmt.Errorf("daemon: durable run state unavailable: %w", persistErr)
	ended := time.Now()
	candidate := r.snapshot()
	candidate.Status = RunFailed
	candidate.Err = failure
	candidate.Error = failure.Error()
	candidate.Message = "run state persistence failed"
	candidate.EndedAt = ended
	candidate.ExecutionFinished = true
	candidate.finalized = true
	if retryErr := rm.persistAndReplace(r, candidate); retryErr != nil {
		failure = fmt.Errorf("%w; retrying failed state also failed: %v", failure, retryErr)
		candidate.Err = failure
		candidate.Error = failure.Error()
		r.replace(candidate)
	}
	snapshot := r.snapshot()
	rm.mailbox.Publish(Event{RunID: snapshot.ID, Kind: EventRunFailed, Time: ended, Err: failure})
	rm.signalActivity()
}

var ErrRunSuperseded = errors.New("daemon: run superseded by a newer push to the same branch")

// SupersedeQueued drops every still-queued (not yet started) job for the
// given (repo, branch) pair from that repo's FIFO, marking each as failed
// with ErrRunSuperseded instead of letting it execute. Only rq.pending is
// inspected, so a job already popped off the queue - running or terminal -
// is left completely alone, matching a fresh push's right to replace a
// stale intent that hasn't started yet, but never a run already underway.
func (rm *RunManager) SupersedeQueued(repo, branch string) error {
	rm.mu.Lock()
	rq, ok := rm.repos[repo]
	rm.mu.Unlock()
	if !ok {
		return nil
	}

	rq.mu.Lock()
	kept := make([]*queuedJob, 0, len(rq.pending))
	var dropped []*queuedJob
	for _, j := range rq.pending {
		if j.run.snapshot().Branch == branch {
			dropped = append(dropped, j)
			continue
		}
		kept = append(kept, j)
	}
	rq.pending = kept
	rq.mu.Unlock()

	now := time.Now()
	var firstErr error
	for _, j := range dropped {
		j.run.persistMu.Lock()
		candidate := j.run.snapshot()
		candidate.Status = RunSuperseded
		candidate.Err = ErrRunSuperseded
		candidate.Error = ErrRunSuperseded.Error()
		candidate.ExecutionFinished = true
		candidate.EndedAt = now
		err := rm.persistAndReplace(j.run, candidate)
		j.run.persistMu.Unlock()
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			rm.failAfterPersistenceError(j.run, err)
			continue
		}
		rm.mailbox.Publish(Event{RunID: j.run.snapshot().ID, Kind: EventRunCanceled, Time: now, Err: ErrRunSuperseded})
		rm.signalActivity()
	}
	return firstErr
}

func (rm *RunManager) cancelQueued(target *run) (bool, error) {
	snapshot := target.snapshot()
	rm.mu.Lock()
	rq := rm.repos[snapshot.Repo]
	rm.mu.Unlock()
	if rq == nil {
		return rm.cancelQueuedRun(target)
	}
	rq.mu.Lock()
	removed := false
	for i, job := range rq.pending {
		if job.run == target {
			rq.pending = append(rq.pending[:i], rq.pending[i+1:]...)
			removed = true
			break
		}
	}
	if !removed {
		active := rq.active
		rq.mu.Unlock()
		if active {
			return false, nil
		}
		return rm.cancelQueuedRun(target)
	}
	rq.mu.Unlock()
	return rm.cancelQueuedRun(target)
}

func (rm *RunManager) cancelQueuedRun(target *run) (bool, error) {
	snapshot := target.snapshot()
	now := time.Now()
	target.persistMu.Lock()
	candidate := target.snapshot()
	if candidate.Status != RunQueued {
		target.persistMu.Unlock()
		return false, nil
	}
	target.cancel()
	candidate.Status = RunCanceled
	candidate.Err = context.Canceled
	candidate.Error = context.Canceled.Error()
	candidate.EndedAt = now
	candidate.ExecutionFinished = true
	err := rm.persistAndReplace(target, candidate)
	target.persistMu.Unlock()
	if err != nil {
		rm.failAfterPersistenceError(target, err)
		return true, err
	}
	rm.mailbox.Publish(Event{RunID: snapshot.ID, Kind: EventRunCanceled, Time: now, Err: context.Canceled})
	rm.signalActivity()
	return true, nil
}

func (rm *RunManager) persistAndReplace(r *run, snapshot RunSnapshot) error {
	rm.durableMu.Lock()
	defer rm.durableMu.Unlock()
	rm.mu.Lock()
	err := rm.persistSnapshotLocked(snapshot)
	rm.mu.Unlock()
	if err != nil {
		return err
	}
	r.replace(snapshot)
	return nil
}

func (rm *RunManager) HasActiveRuns() bool {
	for _, snapshot := range rm.List() {
		if snapshot.Status == RunQueued || snapshot.Status == RunRunning || snapshot.Status == RunAwaitingMerge {
			return true
		}
	}
	return false
}
