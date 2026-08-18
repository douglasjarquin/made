package daemon

import (
	"sync"
	"time"
)

type EventKind string

const (
	EventRunStarted    EventKind = "run_started"
	EventStageStarted  EventKind = "stage_started"
	EventStageFinished EventKind = "stage_finished"
	EventRunCompleted  EventKind = "run_completed"
	EventRunFailed     EventKind = "run_failed"
	EventRunCanceled   EventKind = "run_canceled"
)

type Event struct {
	RunID   string
	Kind    EventKind
	Stage   string
	Message string
	Err     error
	Time    time.Time
}

// mailboxBufferSize: a full pipeline run emits well under 100 events
// (started/finished per stage plus run-level start/complete/fail), so 256
// gives a normal subscriber headroom it will never approach; the bound
// exists only to cap a stalled subscriber's memory.
const mailboxBufferSize = 256

// Mailbox does not replay history: subscribe before submitting the run.
type Mailbox struct {
	mu   sync.Mutex
	subs map[string][]chan Event
}

func NewMailbox() *Mailbox {
	return &Mailbox{subs: make(map[string][]chan Event)}
}

func (m *Mailbox) Subscribe(runID string) (<-chan Event, func()) {
	ch := make(chan Event, mailboxBufferSize)

	m.mu.Lock()
	m.subs[runID] = append(m.subs[runID], ch)
	m.mu.Unlock()

	var once sync.Once
	unsubscribe := func() {
		once.Do(func() {
			m.mu.Lock()
			defer m.mu.Unlock()
			list := m.subs[runID]
			for i, c := range list {
				if c == ch {
					m.subs[runID] = append(list[:i], list[i+1:]...)
					break
				}
			}
			if len(m.subs[runID]) == 0 {
				delete(m.subs, runID)
			}
		})
	}

	return ch, unsubscribe
}

// Publish fans an event out to every subscriber currently on ev.RunID. A
// subscriber channel is never closed here - closing while a send to it may
// still be in flight elsewhere would risk a send-on-closed-channel panic -
// so consumers should treat a terminal event (EventRunCompleted /
// EventRunFailed) as their own signal to stop reading, not channel closure.
func (m *Mailbox) Publish(ev Event) {
	m.mu.Lock()
	subs := append([]chan Event(nil), m.subs[ev.RunID]...)
	m.mu.Unlock()

	for _, ch := range subs {
		select {
		case ch <- ev:
		default:
			// Buffer exhausted: this subscriber fell far behind normal
			// load. Drop rather than block the run that is publishing.
		}
	}
}
