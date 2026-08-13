package api_test

import "testing"

func TestClient_PingPong(t *testing.T) {
	_, client := startTestServer(t)

	var out struct {
		Message string `json:"message"`
	}
	if err := client.CallInto("ping", nil, &out); err != nil {
		t.Fatalf("CallInto ping: %v", err)
	}
	if out.Message != "pong" {
		t.Fatalf("expected ping to return pong, got %q", out.Message)
	}
}
