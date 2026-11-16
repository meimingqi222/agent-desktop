package ipc

import (
	"context"
	"fmt"
	"net"
)

// Handler executes one forwarded command inside the worker session and
// returns the agent-desktop envelope bytes to send back.
type Handler func(ctx context.Context, command string, args []string) ([]byte, error)

// Server is the worker end of the named pipe.
type Server struct {
	listener      net.Listener
	handler       Handler
	trustedClient uint32
}

// NewServer binds a pipe and trusts only the given client PID
// (the host process that launched the worker). trustedPID of 0 keeps
// the check off for local development.
func NewServer(pipeName string, trustedPID uint32, h Handler) (*Server, error) {
	l, err := ListenPipe(pipeName)
	if err != nil {
		return nil, err
	}
	return &Server{listener: l, handler: h, trustedClient: trustedPID}, nil
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
		s.serveConn(ctx, conn)
	}
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

// Close stops accepting.
func (s *Server) Close() error {
	return s.listener.Close()
}

var errUnsupportedPeer = fmt.Errorf("peer PID check unsupported on this connection")
