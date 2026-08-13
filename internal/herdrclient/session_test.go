package herdrclient

import (
	"context"
	"testing"
	"time"
)

// This is the load-bearing test for the hard security constraint in
// consigliere's cs-brief.sh:335 - no herdr call may be scoped only by
// ambient HERDR_SESSION. It inspects the raw JSON payload the fake server
// received, not just the client's Go-level arguments, so a regression that
// dropped the field before serialization would still be caught.
func TestEveryOutgoingCallIncludesExplicitNonEmptySession(t *testing.T) {
	srv := newFakeHerdrServer(t, RequiredProtocolVersion)

	res := connect(context.Background(), srv.socketPath(), SessionName, RequiredProtocolVersion)
	if res.State != StateReady {
		t.Fatalf("setup: State = %v, want StateReady", res.State)
	}
	client := res.Client

	pane, err := client.OpenPane(context.Background(), OpenPaneOptions{Label: "gate-run"})
	if err != nil {
		t.Fatalf("OpenPane: %v", err)
	}

	tailCtx, cancel := context.WithTimeout(context.Background(), 800*time.Millisecond)
	defer cancel()
	ch, err := pane.Tail(tailCtx)
	if err != nil {
		t.Fatalf("Tail: %v", err)
	}
	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for tailed output")
	}
	for range ch {
	}

	if err := pane.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}

	payloads := srv.recordedPayloads()
	if len(payloads) == 0 {
		t.Fatal("no requests reached the fake herdr server")
	}

	seenMethods := map[string]bool{}
	for _, payload := range payloads {
		method, _ := payload["method"].(string)
		seenMethods[method] = true

		session, ok := payload["session"]
		if !ok {
			t.Fatalf("request %v has no session field at all", payload)
		}
		sessionStr, ok := session.(string)
		if !ok || sessionStr == "" {
			t.Fatalf("request %v session field is not a non-empty string: %v", payload, session)
		}
		if sessionStr != SessionName {
			t.Fatalf("request %v session = %q, want %q", payload, sessionStr, SessionName)
		}
	}

	for _, wantMethod := range []string{"ping", "workspace.create", "pane.read", "pane.close"} {
		if !seenMethods[wantMethod] {
			t.Fatalf("expected method %q to have been exercised by this test", wantMethod)
		}
	}
}

func TestConnectItselfNeverReadsAmbientHerdrSessionEnv(t *testing.T) {
	t.Setenv("HERDR_SESSION", "some-other-soldier-session")

	srv := newFakeHerdrServer(t, RequiredProtocolVersion)
	res := connect(context.Background(), srv.socketPath(), SessionName, RequiredProtocolVersion)
	if res.State != StateReady {
		t.Fatalf("State = %v, want StateReady", res.State)
	}

	payloads := srv.recordedPayloads()
	if len(payloads) == 0 {
		t.Fatal("no requests reached the fake herdr server")
	}
	if got := payloads[0]["session"]; got != SessionName {
		t.Fatalf("session = %v, want %q even with HERDR_SESSION set in the environment", got, SessionName)
	}
}
