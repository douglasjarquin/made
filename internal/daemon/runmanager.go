package daemon

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

type RunStatus string

const (
	RunQueued    RunStatus = "queued"
	RunRunning   RunStatus = "running"
	RunCompleted RunStatus = "completed"
	RunFailed    RunStatus = "failed"
)

type RunSnapshot struct {
	ID              string
	Repo            string
	Branch          string
	Status          RunStatus
	QueuedAt        time.Time
	StartedAt       time.Time
	EndedAt         time.Time
	Err             error
	Stages          []StageResult
	PendingFindings []AskUserFinding
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
	return r.snap
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
	mailbox  *Mailbox
	activity chan struct{}

	mu      sync.Mutex
	repos   map[string]*repoQueue
	runs    map[string]*run
	counter uint64
}

func NewRunManager() *RunManager {
	return &RunManager{
		mailbox:  NewMailbox(),
		activity: make(chan struct{}, 1),
		repos:    make(map[string]*repoQueue),
		runs:     make(map[string]*run),
	}
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
	n := atomic.AddUint64(&rm.counter, 1)
	return fmt.Sprintf("run-%d", n)
}

func (rm *RunManager) Submit(id, repo, branch string, work WorkFunc) (RunSnapshot, error) {
	ctx, cancel := context.WithCancel(context.Background())
	r := &run{
		ctx:    ctx,
		cancel: cancel,
		snap: RunSnapshot{
			ID:       id,
			Repo:     repo,
			Branch:   branch,
			Status:   RunQueued,
			QueuedAt: time.Now(),
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
		s.Err = err
		if err != nil {
			s.Status = RunFailed
		} else {
			s.Status = RunCompleted
		}
	})

	finalKind := EventRunCompleted
	if err != nil {
		finalKind = EventRunFailed
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
	return snaps
}

func (rm *RunManager) Subscribe(id string) (<-chan Event, func()) {
	return rm.mailbox.Subscribe(id)
}

// Cancel signals the run's WorkFunc via its context; cancellation surfaces as
// the existing RunFailed status with Err wrapping context.Canceled rather
// than a new status value, since a cooperating WorkFunc returning ctx.Err()
// already distinguishes it from an ordinary failure for any caller checking
// errors.Is(snap.Err, context.Canceled).
func (rm *RunManager) Cancel(id string) error {
	r, ok := rm.lookupRun(id)
	if !ok {
		return fmt.Errorf("daemon: no run %q", id)
	}
	if isTerminalRunStatus(r.snapshot().Status) {
		return fmt.Errorf("daemon: run %q is already %s", id, r.snapshot().Status)
	}
	r.cancel()
	return nil
}

func isTerminalRunStatus(s RunStatus) bool {
	return s == RunCompleted || s == RunFailed
}
