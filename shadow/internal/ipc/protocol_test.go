package ipc

import (
	"bytes"
	"net"
	"testing"
)

func TestFrameRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	req := NewRequest("snapshot", []string{"--app", "notepad.exe"})
	if err := WriteFrame(&buf, req); err != nil {
		t.Fatal(err)
	}
	var got Request
	if err := ReadFrame(&buf, &got); err != nil {
		t.Fatal(err)
	}
	if got.ID != req.ID || got.Command != "snapshot" || got.Args[0] != "--app" {
		t.Fatalf("mismatch: %+v", got)
	}
}

func TestFrameRejectsOversizedLength(t *testing.T) {
	buf := bytes.NewBuffer([]byte{0xff, 0xff, 0xff, 0xff})
	var v any
	if err := ReadFrame(buf, &v); err == nil {
		t.Fatal("expected error for oversized frame")
	}
}

func TestClientRoundTrip(t *testing.T) {
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()
	go func() {
		var req Request
		if err := ReadFrame(c2, &req); err != nil {
			return
		}
		_ = WriteFrame(c2, &Response{ID: req.ID, OK: true, Payload: []byte(`{"ok":true}`)})
	}()
	client := &Client{conn: c1}
	resp, err := client.Call(t.Context(), "status", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !resp.OK || string(resp.Payload) != `{"ok":true}` {
		t.Fatalf("bad response: %+v", resp)
	}
}
