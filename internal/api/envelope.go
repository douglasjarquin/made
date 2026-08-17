package api

import (
	"encoding/json"
	"fmt"
	"path/filepath"
)

const Version = 1

const (
	ErrProtocolMismatch = "protocol_mismatch"
	ErrInvalidRequest   = "invalid_request"
	ErrUnknownMethod    = "unknown_method"
	ErrHandlerFailed    = "handler_error"
	ErrInternal         = "internal_error"
)

type Request struct {
	Protocol int             `json:"made.protocol"`
	ID       string          `json:"id"`
	Method   string          `json:"method"`
	Params   json.RawMessage `json:"params,omitempty"`
}

type Response struct {
	Protocol int             `json:"made.protocol"`
	ID       string          `json:"id"`
	Result   json.RawMessage `json:"result,omitempty"`
	Error    *Error          `json:"error,omitempty"`
}

type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *Error) Error() string {
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

func SocketPath(madeHome string) string {
	return filepath.Join(madeHome, "daemon.sock")
}
