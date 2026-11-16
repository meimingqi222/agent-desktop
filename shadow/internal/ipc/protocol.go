// Package ipc is the named-pipe wire protocol between the ad host and
// the in-session worker: a u32 little-endian length prefix followed by
// one JSON object.
package ipc

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"sync/atomic"
)

// MaxFrame caps one message at 64 MiB; snapshots can be large.
const MaxFrame = 64 << 20

// Request is one host→worker command invocation.
type Request struct {
	ID      uint64   `json:"id"`
	Command string   `json:"command"`
	Args    []string `json:"args,omitempty"`
}

// Response is the worker's reply. Payload is the agent-desktop JSON
// envelope, passed through untouched.
type Response struct {
	ID      uint64          `json:"id"`
	OK      bool            `json:"ok"`
	Payload json.RawMessage `json:"payload,omitempty"`
	Error   string          `json:"error,omitempty"`
}

var nextID atomic.Uint64

// NewRequest mints a request id.
func NewRequest(command string, args []string) *Request {
	return &Request{ID: nextID.Add(1), Command: command, Args: args}
}

// WriteFrame writes one length-prefixed JSON frame.
func WriteFrame(w io.Writer, v any) error {
	body, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if len(body) > MaxFrame {
		return fmt.Errorf("frame of %d bytes exceeds %d cap", len(body), MaxFrame)
	}
	var hdr [4]byte
	binary.LittleEndian.PutUint32(hdr[:], uint32(len(body)))
	if _, err := w.Write(hdr[:]); err != nil {
		return err
	}
	_, err = w.Write(body)
	return err
}

// ReadFrame reads one length-prefixed JSON frame into v.
func ReadFrame(r io.Reader, v any) error {
	var hdr [4]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return err
	}
	n := binary.LittleEndian.Uint32(hdr[:])
	if n == 0 || n > MaxFrame {
		return fmt.Errorf("invalid frame length %d", n)
	}
	body := make([]byte, n)
	if _, err := io.ReadFull(r, body); err != nil {
		return err
	}
	return json.Unmarshal(body, v)
}
