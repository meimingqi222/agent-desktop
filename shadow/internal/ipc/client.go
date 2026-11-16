package ipc

import (
	"context"
	"fmt"
	"io"
	"sync"
)

// Client is the host side of the pipe: one request in flight at a
// time, matched to its response by id. After a timed-out call the
// client is dead: the orphaned read could desynchronize later replies.
type Client struct {
	conn   io.ReadWriteCloser
	mu     sync.Mutex
	broken bool
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
	if c.broken {
		return nil, fmt.Errorf("pipe client is closed")
	}
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
		c.broken = true
		c.conn.Close()
		return nil, ctx.Err()
	case r := <-done:
		if r.err != nil {
			c.broken = true
			return nil, fmt.Errorf("pipe read: %w", r.err)
		}
		if r.resp.ID != req.ID {
			c.broken = true
			return nil, fmt.Errorf("response id %d does not match request %d", r.resp.ID, req.ID)
		}
		if !r.resp.OK {
			return nil, fmt.Errorf("worker error: %s", r.resp.Error)
		}
		return r.resp, nil
	}
}

// Close shuts the pipe. The conn is closed before taking the mutex so
// a Call blocked on a stalled worker is interrupted instead of pinning
// shutdown behind the call lock.
func (c *Client) Close() error {
	var err error
	if c.conn != nil {
		err = c.conn.Close()
	}
	c.mu.Lock()
	c.broken = true
	c.mu.Unlock()
	return err
}
