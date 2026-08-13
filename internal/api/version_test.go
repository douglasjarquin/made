package api_test

import (
	"testing"

	"github.com/douglasjarquin/made/internal/api"
)

func TestServer_RejectsProtocolVersionMismatch(t *testing.T) {
	_, client := startTestServer(t)

	resp, err := client.Send(api.Request{
		Protocol: api.Version + 1,
		ID:       "req-1",
		Method:   "ping",
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if resp.Error == nil {
		t.Fatal("expected a structured error for protocol mismatch, got nil")
	}
	if resp.Error.Code != api.ErrProtocolMismatch {
		t.Fatalf("expected error code %q, got %q", api.ErrProtocolMismatch, resp.Error.Code)
	}
	if resp.ID != "req-1" {
		t.Fatalf("expected response id to echo the request id, got %q", resp.ID)
	}
}
