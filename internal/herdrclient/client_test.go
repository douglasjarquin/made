package herdrclient

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestConnectReadyOnProtocolMatch(t *testing.T) {
	srv := newFakeHerdrServer(t, RequiredProtocolVersion)

	res := connect(context.Background(), srv.socketPath(), SessionName, RequiredProtocolVersion)

	if res.State != StateReady {
		t.Fatalf("State = %v, want StateReady (err=%v)", res.State, res.Err)
	}
	if res.Client == nil {
		t.Fatal("Client = nil, want a usable client when ready")
	}
	if res.Protocol != RequiredProtocolVersion {
		t.Fatalf("Protocol = %d, want %d", res.Protocol, RequiredProtocolVersion)
	}
}

func TestConnectDegradesOnProtocolMismatch(t *testing.T) {
	srv := newFakeHerdrServer(t, RequiredProtocolVersion+1)

	res := connect(context.Background(), srv.socketPath(), SessionName, RequiredProtocolVersion)

	if res.State != StateIncompatible {
		t.Fatalf("State = %v, want StateIncompatible (err=%v)", res.State, res.Err)
	}
	if res.Client != nil {
		t.Fatal("Client != nil, want no usable client on protocol mismatch")
	}
	if res.Err != nil {
		t.Fatalf("Err = %v, want nil: a protocol mismatch is a degraded result, not a generic error a caller must abort on", res.Err)
	}
	if res.Protocol != RequiredProtocolVersion+1 {
		t.Fatalf("Protocol = %d, want %d (server's advertised version)", res.Protocol, RequiredProtocolVersion+1)
	}
}

func TestConnectUnavailableWhenNothingListening(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "nothing-here.sock")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	start := time.Now()
	res := connect(ctx, socketPath, SessionName, RequiredProtocolVersion)
	elapsed := time.Since(start)

	if res.State != StateUnavailable {
		t.Fatalf("State = %v, want StateUnavailable (err=%v)", res.State, res.Err)
	}
	if res.Client != nil {
		t.Fatal("Client != nil, want no usable client when herdr is unavailable")
	}
	if res.Err == nil {
		t.Fatal("Err = nil, want a dial error explaining unavailability for logging")
	}
	if elapsed > 3*time.Second {
		t.Fatalf("connect took %v, want a short bounded dial timeout instead of hanging", elapsed)
	}
}

func TestConnectUnavailableAndIncompatibleAreDistinguishable(t *testing.T) {
	srv := newFakeHerdrServer(t, RequiredProtocolVersion+5)
	incompatible := connect(context.Background(), srv.socketPath(), SessionName, RequiredProtocolVersion)

	socketPath := filepath.Join(t.TempDir(), "nothing-here.sock")
	unavailable := connect(context.Background(), socketPath, SessionName, RequiredProtocolVersion)

	if incompatible.State == unavailable.State {
		t.Fatalf("expected distinguishable states, got %v for both", incompatible.State)
	}
}
