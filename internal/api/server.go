package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"sync"
)

type HandlerFunc func(ctx context.Context, params json.RawMessage) (any, error)

type Server struct {
	socketPath string
	ln         net.Listener

	mu       sync.RWMutex
	handlers map[string]HandlerFunc
}

func NewServer(socketPath string) *Server {
	s := &Server{
		socketPath: socketPath,
		handlers:   make(map[string]HandlerFunc),
	}
	s.Handle("ping", handlePing)
	return s
}

func (s *Server) Handle(method string, h HandlerFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.handlers[method] = h
}

// Listen creates the unix socket with 0600 permissions - owner-only access
// is made's entire auth model for this socket, matching herdr's own
// filesystem-permission-only model, so there is no separate credential check.
func (s *Server) Listen() error {
	if err := os.RemoveAll(s.socketPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove stale socket %s: %w", s.socketPath, err)
	}

	ln, err := net.Listen("unix", s.socketPath)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", s.socketPath, err)
	}
	if err := os.Chmod(s.socketPath, 0o600); err != nil {
		_ = ln.Close()
		return fmt.Errorf("chmod %s: %w", s.socketPath, err)
	}

	s.ln = ln
	return nil
}

func (s *Server) Serve(ctx context.Context) error {
	if s.ln == nil {
		return errors.New("api: Listen must be called before Serve")
	}

	go func() {
		<-ctx.Done()
		_ = s.ln.Close()
	}()

	for {
		conn, err := s.ln.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return nil
			default:
				return fmt.Errorf("accept: %w", err)
			}
		}
		go s.serveConn(ctx, conn)
	}
}

func (s *Server) Close() error {
	if s.ln == nil {
		return nil
	}
	err := s.ln.Close()
	_ = os.Remove(s.socketPath)
	return err
}

func (s *Server) serveConn(ctx context.Context, conn net.Conn) {
	defer func() { _ = conn.Close() }()

	dec := json.NewDecoder(conn)
	enc := json.NewEncoder(conn)
	for {
		var req Request
		if err := dec.Decode(&req); err != nil {
			return
		}
		if err := enc.Encode(s.dispatch(ctx, req)); err != nil {
			return
		}
	}
}

// dispatch enforces made's exact-match protocol version policy - mirroring
// herdr's own check_client_version - before it ever looks up a handler.
// Client/daemon version skew is a correctness risk made controls
// end-to-end, so it fails closed rather than negotiating a floor.
func (s *Server) dispatch(ctx context.Context, req Request) Response {
	if req.Protocol != Version {
		return errorResponse(req.ID, ErrProtocolMismatch, fmt.Sprintf(
			"client protocol %d does not match server protocol %d", req.Protocol, Version))
	}

	s.mu.RLock()
	h, ok := s.handlers[req.Method]
	s.mu.RUnlock()
	if !ok {
		return errorResponse(req.ID, ErrUnknownMethod, fmt.Sprintf("unknown method %q", req.Method))
	}

	result, err := h(ctx, req.Params)
	if err != nil {
		return errorResponse(req.ID, ErrHandlerFailed, err.Error())
	}

	raw, err := json.Marshal(result)
	if err != nil {
		return errorResponse(req.ID, ErrInternal, err.Error())
	}
	return Response{Protocol: Version, ID: req.ID, Result: raw}
}

func errorResponse(id, code, message string) Response {
	return Response{Protocol: Version, ID: id, Error: &Error{Code: code, Message: message}}
}

type pingResult struct {
	Message string `json:"message"`
}

func handlePing(context.Context, json.RawMessage) (any, error) {
	return pingResult{Message: "pong"}, nil
}
