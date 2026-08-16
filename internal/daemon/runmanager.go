package daemon

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
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
	RunFailed         RunStatus = "failed"
	RunCanceled       RunStatus = "canceled"
	RunSuperseded     RunStatus = "superseded"
)

type RunSnapshot struct {
	ID                string
	Repo              string
	Branch            string
	InputSHA          string
	OutputSHA         string
	Status            RunStatus
	QueuedAt          time.Time
	StartedAt         time.Time
	EndedAt           time.Time
	ExecutionFinished bool
	Err               error
	Errors            []string
	Message           string
	Findings          []RunFinding
	Decisions         map[string]string
	PRURL             string
	SupersededBy      string
	CancelRequested   bool
	SubmissionEvents  []SubmissionEvent
	Stages            []StageResult
	PendingFindings   []AskUserFinding

	// finalized is set by Finish and read by execute: it lets a WorkFunc
	// declare a run's definitive terminal-or-not Status/Message itself,
	// overriding execute's normal "nil error means RunSucceeded" inference -
	// needed for the orchestrator's CI-passed-but-awaiting-human-merge case,
	// where the pipeline finished successfully yet the run must stay
	// RunAwaitingMerge rather than flip to RunSucceeded.
	finalized bool
}

type WorkFunc func(ctx context.Context, emit func(Event)) error

var ErrRunIDExists = errors.New("daemon: run ID already submitted")

type run struct {
	mu     sync.Mutex
	snap   RunSnapshot
	ctx    context.Context
	cancel context.CancelFunc
}

func (r *run) snapshot() RunSnapshot {
	r.mu.Lock()
	defer r.mu.Unlock()
	return cloneSnapshot(r.snap)
}

func (r *run) update(fn func(*RunSnapshot)) {
	r.mu.Lock()
	fn(&r.snap)
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
	mailbox   *Mailbox
	activity  chan struct{}
	store     *RunStore
	persistMu sync.Mutex

	mu    sync.Mutex
	repos map[string]*repoQueue
	runs  map[string]*run
}

func NewRunManager() *RunManager {
	return newRunManager(nil, nil)
}

func NewPersistentRunManager(path string) (*RunManager, error) {
	store, snapshots, err := OpenRunStore(path)
	if err != nil {
		return nil, err
	}
	rm := newRunManager(store, snapshots)
	if err := rm.reconcileRestoredRuns(); err != nil {
		return nil, err
	}
	return rm, nil
}

func newRunManager(store *RunStore, snapshots map[string]RunSnapshot) *RunManager {
	rm := &RunManager{
		mailbox:  NewMailbox(),
		activity: make(chan struct{}, 1),
		repos:    make(map[string]*repoQueue),
		runs:     make(map[string]*run),
		store:    store,
	}
	for id, snapshot := range snapshots {
		ctx, cancel := context.WithCancel(context.Background())
		rm.runs[id] = &run{ctx: ctx, cancel: cancel, snap: cloneSnapshot(snapshot)}
	}
	return rm
}

func (rm *RunManager) persist(r *run) error {
	if rm.store == nil {
		return nil
	}
	rm.persistMu.Lock()
	defer rm.persistMu.Unlock()
	return rm.store.Append(r.snapshot())
}

func (rm *RunManager) reconcileRestoredRuns() error {
	for _, r := range rm.runs {
		snapshot := r.snapshot()
		if snapshot.Status != RunQueued && snapshot.Status != RunRunning && snapshot.Status != RunAwaitingReview {
			continue
		}
		restartedErr := errors.New("daemon restarted before execution finished")
		r.update(func(s *RunSnapshot) {
			s.Status = RunFailed
			s.EndedAt = time.Now()
			s.ExecutionFinished = true
			s.Err = restartedErr
			s.Errors = append(s.Errors, restartedErr.Error())
		})
		if err := rm.persist(r); err != nil {
			return fmt.Errorf("reconcile restored run %q: %w", snapshot.ID, err)
		}
	}
	return nil
}

func (rm *RunManager) ActivitySignal() <-chan struct{} {
	return rm.activity
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
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		counter := atomic.AddUint64(&fallbackRunIDCounter, 1)
		for i := range id {
			id[i] = byte(counter >> (uint(i%8) * 8))
		}
	}
	id[6] = (id[6] & 0x0f) | 0x40
	id[8] = (id[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", id[0:4], id[4:6], id[6:8], id[8:10], id[10:16])
}

var fallbackRunIDCounter uint64

func (rm *RunManager) Submit(id, repo, branch string, work WorkFunc) (RunSnapshot, error) {
	return rm.SubmitWithMetadata(id, repo, branch, "", "", work)
}

func (rm *RunManager) SubmitWithMetadata(id, repo, branch, inputSHA, outputSHA string, work WorkFunc) (RunSnapshot, error) {
	ctx, cancel := context.WithCancel(context.Background())
	r := &run{
		ctx:    ctx,
		cancel: cancel,
		snap: RunSnapshot{
			ID:               id,
			Repo:             repo,
			Branch:           branch,
			InputSHA:         inputSHA,
			OutputSHA:        outputSHA,
			Status:           RunQueued,
			QueuedAt:         time.Now(),
			Errors:           []string{},
			Findings:         []RunFinding{},
			Decisions:        make(map[string]string),
			SubmissionEvents: []SubmissionEvent{},
		},
	}

	rm.mu.Lock()
	if _, exists := rm.runs[id]; exists {
		rm.mu.Unlock()
		return RunSnapshot{}, ErrRunIDExists
	}
	rm.runs[id] = r
	rq, ok := rm.repos[repo]
	if !ok {
		rq = &repoQueue{}
		rm.repos[repo] = rq
	}
	rm.mu.Unlock()
	if err := rm.persist(r); err != nil {
		rm.mu.Lock()
		delete(rm.runs, id)
		if current, ok := rm.repos[repo]; ok && current == rq {
			delete(rm.repos, repo)
		}
		rm.mu.Unlock()
		cancel()
		return RunSnapshot{}, fmt.Errorf("persist submitted run: %w", err)
	}

	rq.mu.Lock()
	rq.pending = append(rq.pending, &queuedJob{run: r, work: work})
	startDrain := !rq.active
	rq.active = true
	rq.mu.Unlock()

	if startDrain {
		go rm.drain(rq)
	}

	return r.snapshot(), nil
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
	id := r.snapshot().ID
	started := time.Now()
	r.update(func(s *RunSnapshot) {
		s.Status = RunRunning
		s.StartedAt = started
	})
	if err := rm.persist(r); err != nil {
		retryErr := rm.recordPersistenceFailure(r, err)
		eventErr := err
		if retryErr != nil {
			eventErr = errors.Join(err, retryErr)
		}
		rm.mailbox.Publish(Event{RunID: id, Kind: EventRunFailed, Time: time.Now(), Err: eventErr})
		return
	}
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

	err := work(r.ctx, emit)
	rm.signalActivity()

	ended := time.Now()
	r.update(func(s *RunSnapshot) {
		s.EndedAt = ended
		s.ExecutionFinished = true
		if s.finalized {
			return
		}
		s.Err = err
		if errors.Is(err, context.Canceled) || s.CancelRequested {
			s.Status = RunCanceled
			if err != nil {
				s.Errors = append(s.Errors, err.Error())
			}
		} else if err != nil {
			s.Status = RunFailed
			s.Errors = append(s.Errors, err.Error())
		} else {
			s.Status = RunSucceeded
		}
	})
	if persistErr := rm.persist(r); persistErr != nil {
		retryErr := rm.recordPersistenceFailure(r, persistErr)
		if retryErr != nil {
			err = errors.Join(err, persistErr, retryErr)
		} else {
			err = errors.Join(err, persistErr)
		}
	}

	finalKind := EventRunCompleted
	if err != nil {
		finalKind = EventRunFailed
	}
	rm.mailbox.Publish(Event{RunID: id, Kind: finalKind, Time: ended, Err: err})
}

func (rm *RunManager) recordPersistenceFailure(r *run, persistErr error) error {
	durabilityErr := fmt.Errorf("daemon: durable run state write failed: %w", persistErr)
	r.update(func(s *RunSnapshot) {
		s.Status = RunFailed
		s.EndedAt = time.Now()
		s.ExecutionFinished = true
		s.Err = durabilityErr
		s.Errors = append(s.Errors, durabilityErr.Error())
	})
	retryErr := rm.persist(r)
	if retryErr != nil {
		r.update(func(s *RunSnapshot) {
			s.Errors = append(s.Errors, fmt.Sprintf("daemon: retry durable run state write failed: %v", retryErr))
		})
	}
	return retryErr
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
		snaps[i] = cloneSnapshot(r.snapshot())
	}
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
	if snapshot.Status == RunCanceled {
		return nil
	}
	if isTerminalRunStatus(snapshot.Status) {
		return fmt.Errorf("daemon: run %q is already %s", id, snapshot.Status)
	}
	if snapshot.CancelRequested {
		r.cancel()
		if snapshot.Status == RunAwaitingMerge || snapshot.Status == RunAwaitingReview {
			r.update(func(s *RunSnapshot) {
				s.Status = RunCanceled
				s.ExecutionFinished = true
				s.EndedAt = time.Now()
				s.Err = context.Canceled
			})
			if err := rm.persist(r); err != nil {
				return fmt.Errorf("persist canceled run: %w", err)
			}
		}
		return nil
	}
	r.update(func(s *RunSnapshot) { s.CancelRequested = true })
	if err := rm.persist(r); err != nil {
		r.cancel()
		return fmt.Errorf("persist cancellation request: %w", err)
	}
	if snapshot.Status == RunAwaitingMerge || snapshot.Status == RunAwaitingReview {
		r.update(func(s *RunSnapshot) {
			s.Status = RunCanceled
			s.ExecutionFinished = true
			s.EndedAt = time.Now()
			s.Err = context.Canceled
			s.Errors = append(s.Errors, context.Canceled.Error())
		})
		r.cancel()
		if err := rm.persist(r); err != nil {
			return fmt.Errorf("persist canceled run: %w", err)
		}
		return nil
	}
	r.cancel()
	return nil
}

func isTerminalRunStatus(s RunStatus) bool {
	return s == RunSucceeded || s == RunFailed || s == RunCanceled || s == RunSuperseded
}

// Finish lets a WorkFunc declare a run's definitive Status and a
// human-readable Message just before it returns, so execute's normal
// nil-error-means-RunSucceeded inference does not overwrite it (see
// RunSnapshot.finalized). status may be any RunStatus, including RunRunning
// for a run that must stay open pending action made cannot itself take.
func (rm *RunManager) Finish(id string, status RunStatus, message string) error {
	r, ok := rm.lookupRun(id)
	if !ok {
		return fmt.Errorf("daemon: no run %q", id)
	}
	r.update(func(s *RunSnapshot) {
		s.Status = status
		s.Message = message
		s.finalized = true
	})
	if err := rm.persist(r); err != nil {
		return fmt.Errorf("persist finished run: %w", err)
	}
	return nil
}

// ErrRunSuperseded marks a run SupersedeQueued dropped before it ever
// started, because a newer push to the same branch arrived first.
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
		j.run.update(func(s *RunSnapshot) {
			s.Status = RunSuperseded
			s.Err = ErrRunSuperseded
			s.Errors = append(s.Errors, ErrRunSuperseded.Error())
			s.EndedAt = now
			s.ExecutionFinished = true
		})
		if err := rm.persist(j.run); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("persist superseded run: %w", err)
		}
		rm.mailbox.Publish(Event{RunID: j.run.snapshot().ID, Kind: EventRunFailed, Time: now, Err: ErrRunSuperseded})
		rm.signalActivity()
	}
	return firstErr
}
