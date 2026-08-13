package api

import (
	"encoding/json"
	"fmt"
	"net"
	"strconv"
	"sync"
	"sync/atomic"
)

type Client struct {
	conn net.Conn
	dec  *json.Decoder
	enc  *json.Encoder

	callMu sync.Mutex
	nextID uint64
}

func Dial(socketPath string) (*Client, error) {
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", socketPath, err)
	}
	return &Client{
		conn: conn,
		dec:  json.NewDecoder(conn),
		enc:  json.NewEncoder(conn),
	}, nil
}

func (c *Client) Close() error {
	return c.conn.Close()
}

// Send holds callMu for the full round trip: one connection carries one
// request/response at a time, so concurrent callers are serialized, not
// multiplexed.
func (c *Client) Send(req Request) (Response, error) {
	c.callMu.Lock()
	defer c.callMu.Unlock()

	if err := c.enc.Encode(req); err != nil {
		return Response{}, fmt.Errorf("send request: %w", err)
	}
	var resp Response
	if err := c.dec.Decode(&resp); err != nil {
		return Response{}, fmt.Errorf("read response: %w", err)
	}
	return resp, nil
}

func (c *Client) Call(method string, params any) (json.RawMessage, error) {
	var raw json.RawMessage
	if params != nil {
		b, err := json.Marshal(params)
		if err != nil {
			return nil, fmt.Errorf("marshal params for %s: %w", method, err)
		}
		raw = b
	}

	resp, err := c.Send(Request{
		Protocol: Version,
		ID:       strconv.FormatUint(atomic.AddUint64(&c.nextID, 1), 10),
		Method:   method,
		Params:   raw,
	})
	if err != nil {
		return nil, err
	}
	if resp.Error != nil {
		return nil, resp.Error
	}
	return resp.Result, nil
}

func (c *Client) CallInto(method string, params, out any) error {
	raw, err := c.Call(method, params)
	if err != nil {
		return err
	}
	if out == nil || raw == nil {
		return nil
	}
	return json.Unmarshal(raw, out)
}
