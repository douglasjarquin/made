package herdrclient

import (
	"context"
	"testing"
)

func TestOpenPaneReturnsIdentifiersFromServer(t *testing.T) {
	srv := newFakeHerdrServer(t, RequiredProtocolVersion)
	res := connect(context.Background(), srv.socketPath(), SessionName, RequiredProtocolVersion)
	if res.State != StateReady {
		t.Fatalf("setup: State = %v, want StateReady", res.State)
	}

	pane, err := res.Client.OpenPane(context.Background(), OpenPaneOptions{Label: "gate-run"})
	if err != nil {
		t.Fatalf("OpenPane: %v", err)
	}
	if pane.ID != "pane-1" {
		t.Fatalf("pane.ID = %q, want %q", pane.ID, "pane-1")
	}
	if pane.WorkspaceID != "ws-1" {
		t.Fatalf("pane.WorkspaceID = %q, want %q", pane.WorkspaceID, "ws-1")
	}
	if pane.TabID != "tab-1" {
		t.Fatalf("pane.TabID = %q, want %q", pane.TabID, "tab-1")
	}
}

func TestPaneCloseSucceeds(t *testing.T) {
	srv := newFakeHerdrServer(t, RequiredProtocolVersion)
	res := connect(context.Background(), srv.socketPath(), SessionName, RequiredProtocolVersion)
	if res.State != StateReady {
		t.Fatalf("setup: State = %v, want StateReady", res.State)
	}

	pane, err := res.Client.OpenPane(context.Background(), OpenPaneOptions{})
	if err != nil {
		t.Fatalf("OpenPane: %v", err)
	}

	if err := pane.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}
}
