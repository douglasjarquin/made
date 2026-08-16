package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"sync"
	"syscall"
	"time"
)

type HandlerFunc func(ctx context.Context, params json.RawMessage) (any, error)

type Server struct {
	socketPath string
	ln         net.Listener
	socketInfo os.FileInfo

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
	if info, err := os.Lstat(s.socketPath); err == nil {
		return fmt.Errorf("api: socket path %s already exists as %s", s.socketPath, info.Mode())
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("api: inspect socket path %s: %w", s.socketPath, err)
	}

	ln, err := net.Listen("unix", s.socketPath)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", s.socketPath, err)
	}
	if err := os.Chmod(s.socketPath, 0o600); err != nil {
		_ = ln.Close()
		return fmt.Errorf("chmod %s: %w", s.socketPath, err)
	}
	if unixListener, ok := ln.(*net.UnixListener); ok {
		unixListener.SetUnlinkOnClose(false)
	}

	s.ln = ln
	info, err := os.Lstat(s.socketPath)
	if err != nil {
		_ = ln.Close()
		return fmt.Errorf("stat listened socket %s: %w", s.socketPath, err)
	}
	s.socketInfo = info
	return nil
}

func PrepareSocket(socketPath string) error {
	info, err := os.Lstat(socketPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("api: inspect stale socket %s: %w", socketPath, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || info.IsDir() {
		return fmt.Errorf("api: refusing non-owned socket path %s with mode %s", socketPath, info.Mode())
	}
	if info.Mode()&os.ModeSocket == 0 {
		return fmt.Errorf("api: refusing regular socket path %s", socketPath)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || uint32(stat.Uid) != uint32(os.Getuid()) {
		return fmt.Errorf("api: refusing socket path %s owned by another user", socketPath)
	}
	live, err := socketIsLive(socketPath)
	if err != nil {
		return fmt.Errorf("api: cannot prove owner socket %s is stale: %w", socketPath, err)
	}
	if live {
		return fmt.Errorf("api: refusing to replace live owner socket %s", socketPath)
	}
	if err := os.Remove(socketPath); err != nil {
		return fmt.Errorf("api: remove stale owner socket %s: %w", socketPath, err)
	}
	return nil
}

func socketIsLive(socketPath string) (bool, error) {
	conn, err := net.DialTimeout("unix", socketPath, 100*time.Millisecond)
	if err == nil {
		_ = conn.Close()
		return true, nil
	}
	if errors.Is(err, syscall.ECONNREFUSED) {
		return false, nil
	}
	return false, err
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
	if info, statErr := os.Lstat(s.socketPath); statErr == nil && os.SameFile(s.socketInfo, info) && info.Mode()&os.ModeSocket != 0 {
		_ = os.Remove(s.socketPath)
	}
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
