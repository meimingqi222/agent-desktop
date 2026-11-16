package ipc

import (
	"context"
	"fmt"
	"io"
	"sync"
)

// Client is the host side of the pipe: one request in flight at a
// time, matched to its response by id.
type Client struct {
	conn io.ReadWriteCloser
	mu   sync.Mutex
}

// Dial connects to the worker's named pipe.
func Dial(pipeName string) (*Client, error) {
	conn, err := dialPipe(pipeName)
	if err != nil {
		return nil, err
	}
	return &Client{conn: conn}, nil
}

// Call sends one command and waits for its response.
func (c *Client) Call(ctx context.Context, command string, args []string) (*Response, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	req := NewRequest(command, args)
	if err := WriteFrame(c.conn, req); err != nil {
		return nil, fmt.Errorf("pipe write: %w", err)
	}
	type result struct {
		resp *Response
		err  error
	}
	done := make(chan result, 1)
	go func() {
		var resp Response
		err := ReadFrame(c.conn, &resp)
		done <- result{resp: &resp, err: err}
	}()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case r := <-done:
		if r.err != nil {
			return nil, fmt.Errorf("pipe read: %w", r.err)
		}
		if !r.resp.OK {
			return nil, fmt.Errorf("worker error: %s", r.resp.Error)
		}
		return r.resp, nil
	}
}

// Close shuts the pipe.
func (c *Client) Close() error {
	if c.conn == nil {
		return nil
	}
	return c.conn.Close()
}
