package managed

import (
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"time"
)

// EventWriter writes JSON-Lines events to stdout with a monotonic sequence.
// All human-readable diagnostics must go to stderr; this writer owns stdout.
type EventWriter struct {
	mu     sync.Mutex
	w      io.Writer
	seq    int
	opts   *Options
	closed bool
}

// NewEventWriter creates an EventWriter bound to the given options.
func NewEventWriter(w io.Writer, opts *Options) *EventWriter {
	return &EventWriter{w: w, opts: opts}
}

// Emit writes one event to the output stream.
// It is safe to call from multiple goroutines, but only one goroutine should
// drive the managed runner at a time.
func (ew *EventWriter) Emit(eventType string, payload any) error {
	ew.mu.Lock()
	defer ew.mu.Unlock()
	if ew.closed {
		return fmt.Errorf("events: writer closed; cannot emit %q after terminal event", eventType)
	}
	ew.seq++
	ev := Event{
		SchemaVersion:   SchemaVersion,
		ProtocolVersion: ProtocolVersion,
		Sequence:        ew.seq,
		RunID:           ew.opts.RunID,
		MissionID:       ew.opts.MissionID,
		InputSHA:        ew.opts.InputSHA,
		PolicyHash:      ew.opts.PolicyHash,
		EventType:       eventType,
		Timestamp:       time.Now().UTC(),
		Payload:         payload,
	}
	data, err := json.Marshal(ev)
	if err != nil {
		return fmt.Errorf("events: marshal %q: %w", eventType, err)
	}
	data = append(data, '\n')
	if _, err := ew.w.Write(data); err != nil {
		return fmt.Errorf("events: write %q: %w", eventType, err)
	}
	if isTerminalEvent(eventType) {
		ew.closed = true
	}
	return nil
}

// EmitTerminal emits the run.completed terminal event and seals the writer.
func (ew *EventWriter) EmitTerminal(payload RunCompletedPayload) error {
	return ew.Emit("run.completed", payload)
}

// Sequence returns the current sequence counter (number of events emitted so far).
func (ew *EventWriter) Sequence() int {
	ew.mu.Lock()
	defer ew.mu.Unlock()
	return ew.seq
}

func isTerminalEvent(eventType string) bool {
	return eventType == "run.completed"
}
