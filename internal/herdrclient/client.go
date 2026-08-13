package herdrclient

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"sync/atomic"
	"time"
)

const (
	SessionName             = "made"
	RequiredProtocolVersion = 20

	dialTimeout    = 2 * time.Second
	requestTimeout = 5 * time.Second
)

type State int

const (
	StateUnavailable State = iota
	StateIncompatible
	StateReady
)

func (s State) String() string {
	switch s {
	case StateReady:
		return "ready"
	case StateIncompatible:
		return "incompatible"
	default:
		return "unavailable"
	}
}

type ConnectResult struct {
	State    State
	Client   *Client
	Version  string
	Protocol int
	Err      error
}

func (r ConnectResult) Available() bool {
	return r.State == StateReady
}

type Client struct {
	socketPath string
	session    string
	nextID     atomic.Uint64
}

func Connect(ctx context.Context) ConnectResult {
	return connect(ctx, SocketPath(), SessionName, RequiredProtocolVersion)
}

func connect(ctx context.Context, socketPath, session string, requiredProtocol int) ConnectResult {
	c := &Client{socketPath: socketPath, session: session}

	var pong pongResult
	if err := c.call(ctx, "ping", nil, &pong); err != nil {
		return ConnectResult{State: StateUnavailable, Err: err}
	}

	if pong.Protocol != requiredProtocol {
		return ConnectResult{State: StateIncompatible, Version: pong.Version, Protocol: pong.Protocol}
	}

	return ConnectResult{State: StateReady, Client: c, Version: pong.Version, Protocol: pong.Protocol}
}

func (c *Client) call(ctx context.Context, method string, params any, result any) error {
	dialCtx, cancel := context.WithTimeout(ctx, dialTimeout)
	defer cancel()

	var dialer net.Dialer
	conn, err := dialer.DialContext(dialCtx, "unix", c.socketPath)
	if err != nil {
		return fmt.Errorf("herdrclient: dial %s: %w", c.socketPath, err)
	}
	defer func() { _ = conn.Close() }()

	deadline := time.Now().Add(requestTimeout)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	if err := conn.SetDeadline(deadline); err != nil {
		return fmt.Errorf("herdrclient: set deadline: %w", err)
	}

	req := wireRequest{
		ID:      fmt.Sprintf("made-%d", c.nextID.Add(1)),
		Method:  method,
		Session: c.session,
	}
	if params != nil {
		encoded, err := json.Marshal(params)
		if err != nil {
			return fmt.Errorf("herdrclient: marshal params: %w", err)
		}
		req.Params = encoded
	}

	line, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("herdrclient: marshal request: %w", err)
	}
	if _, err := conn.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("herdrclient: write request: %w", err)
	}

	respLine, err := bufio.NewReader(conn).ReadBytes('\n')
	if err != nil && len(respLine) == 0 {
		return fmt.Errorf("herdrclient: read response: %w", err)
	}

	var resp wireResponse
	if err := json.Unmarshal(respLine, &resp); err != nil {
		return fmt.Errorf("herdrclient: decode response: %w", err)
	}
	if resp.Error != nil {
		return fmt.Errorf("herdrclient: %s: %s", resp.Error.Code, resp.Error.Message)
	}
	if result != nil && len(resp.Result) > 0 {
		if err := json.Unmarshal(resp.Result, result); err != nil {
			return fmt.Errorf("herdrclient: decode result: %w", err)
		}
	}
	return nil
}
