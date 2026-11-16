package ipc

import (
	"context"
	"fmt"
	"net"
	"sync"
)

// Handler executes one forwarded command inside the worker session and
// returns the agent-desktop envelope bytes to send back.
type Handler func(ctx context.Context, command string, args []string) ([]byte, error)

// Server is the worker end of the named pipe.
type Server struct {
	listener      net.Listener
	handler       Handler
	trustedClient uint32
	connsMu       sync.Mutex
	conns         map[net.Conn]struct{}
}

// NewServer binds a pipe and trusts only the given client PID
// (the host process that launched the worker). trustedPID of 0 keeps
// the check off for local development.
func NewServer(pipeName string, trustedPID uint32, h Handler) (*Server, error) {
	l, err := ListenPipe(pipeName)
	if err != nil {
		return nil, err
	}
	return &Server{listener: l, handler: h, trustedClient: trustedPID,
		conns: make(map[net.Conn]struct{})}, nil
}

// PipeName returns the bound pipe path.
func (s *Server) PipeName() string {
	return s.listener.Addr().String()
}

// Serve accepts connections until ctx is cancelled or the listener
// errors. One connection at a time: the host is a single client.
func (s *Server) Serve(ctx context.Context) error {
	go func() {
		<-ctx.Done()
		s.listener.Close()
		s.closeConns()
	}()
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return nil
			default:
				return err
			}
		}
		s.track(conn)
		s.serveConn(ctx, conn)
		s.untrack(conn)
	}
}

func (s *Server) track(conn net.Conn) {
	s.connsMu.Lock()
	defer s.connsMu.Unlock()
	s.conns[conn] = struct{}{}
}

func (s *Server) untrack(conn net.Conn) {
	s.connsMu.Lock()
	defer s.connsMu.Unlock()
	delete(s.conns, conn)
}

func (s *Server) closeConns() {
	s.connsMu.Lock()
	defer s.connsMu.Unlock()
	for c := range s.conns {
		c.Close()
	}
	s.conns = make(map[net.Conn]struct{})
}

func (s *Server) serveConn(ctx context.Context, conn net.Conn) {
	defer conn.Close()
	if s.trustedClient != 0 {
		pid, err := clientPID(conn)
		if err != nil || pid != s.trustedClient {
			return
		}
	}
	for {
		var req Request
		if err := ReadFrame(conn, &req); err != nil {
			return
		}
		payload, err := s.handler(ctx, req.Command, req.Args)
		resp := Response{ID: req.ID}
		if err != nil {
			resp.Error = err.Error()
		} else {
			resp.OK = true
			resp.Payload = payload
		}
		if err := WriteFrame(conn, &resp); err != nil {
			return
		}
	}
}

// Close stops accepting and drops live connections.
func (s *Server) Close() error {
	s.closeConns()
	return s.listener.Close()
}

var errUnsupportedPeer = fmt.Errorf("peer PID check unsupported on this connection")
