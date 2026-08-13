package daemon

import (
	"testing"
	"time"
)

func TestMailbox_SubscriberReceivesInOrder(t *testing.T) {
	mb := NewMailbox()
	ch, unsubscribe := mb.Subscribe("run-1")
	defer unsubscribe()

	mb.Publish(Event{RunID: "run-1", Kind: EventStageStarted, Stage: "intent"})
	mb.Publish(Event{RunID: "run-1", Kind: EventStageFinished, Stage: "intent"})
	mb.Publish(Event{RunID: "run-1", Kind: EventRunCompleted})

	want := []EventKind{EventStageStarted, EventStageFinished, EventRunCompleted}
	for i, k := range want {
		select {
		case ev := <-ch:
			if ev.Kind != k {
				t.Fatalf("event %d: got %q, want %q", i, ev.Kind, k)
			}
		case <-time.After(time.Second):
			t.Fatalf("event %d: timed out", i)
		}
	}
}

func TestMailbox_IgnoresOtherRunIDs(t *testing.T) {
	mb := NewMailbox()
	ch, unsubscribe := mb.Subscribe("run-1")
	defer unsubscribe()

	mb.Publish(Event{RunID: "run-2", Kind: EventRunCompleted})

	select {
	case ev := <-ch:
		t.Fatalf("received event meant for a different run: %+v", ev)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestMailbox_MultipleSubscribersEachGetAllEvents(t *testing.T) {
	mb := NewMailbox()
	ch1, unsub1 := mb.Subscribe("run-1")
	ch2, unsub2 := mb.Subscribe("run-1")
	defer unsub1()
	defer unsub2()

	mb.Publish(Event{RunID: "run-1", Kind: EventRunStarted})

	for _, ch := range []<-chan Event{ch1, ch2} {
		select {
		case ev := <-ch:
			if ev.Kind != EventRunStarted {
				t.Fatalf("got %q, want %q", ev.Kind, EventRunStarted)
			}
		case <-time.After(time.Second):
			t.Fatal("subscriber did not receive event")
		}
	}
}

func TestMailbox_UnsubscribeStopsFutureDelivery(t *testing.T) {
	mb := NewMailbox()
	ch, unsubscribe := mb.Subscribe("run-1")
	unsubscribe()

	mb.Publish(Event{RunID: "run-1", Kind: EventRunStarted})

	select {
	case ev, ok := <-ch:
		if ok {
			t.Fatalf("received event after unsubscribe: %+v", ev)
		}
	case <-time.After(50 * time.Millisecond):
	}
}

func TestMailbox_OverflowDropsRatherThanBlocks(t *testing.T) {
	mb := NewMailbox()
	_, unsubscribe := mb.Subscribe("run-1")
	defer unsubscribe()

	done := make(chan struct{})
	go func() {
		for i := 0; i < mailboxBufferSize*2; i++ {
			mb.Publish(Event{RunID: "run-1", Kind: EventStageStarted})
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Publish blocked instead of dropping once the buffer filled")
	}
}
