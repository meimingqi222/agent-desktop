//go:build windows

package session

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// RdpHost wraps the ad-rdphost helper process. The helper hosts the
// mstscax ActiveX control connected to a WTS child session; the child
// session only stays connected while the helper (and its control) is
// alive, so the host's lifetime is the session's lifetime.
type RdpHost struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader
	errBuf *errLog
}

type errLog struct{ buf []byte }

func (e *errLog) Write(p []byte) (int, error) {
	if len(e.buf) < 8192 {
		e.buf = append(e.buf, p...)
	}
	return len(p), nil
}
func (e *errLog) String() string { return string(e.buf) }

type rdpHostEvent struct {
	Event     string `json:"event"`
	SessionID int    `json:"session_id"`
	Message   string `json:"message"`
}

// spawnRdpHost locates and launches ad-rdphost.exe. AD_RDPHOST_BIN
// overrides the lookup; otherwise the helper is expected next to the
// running binary (both live in shadow/bin). width/height of 0 leave
// the helper's 1600x900 default. viewer shows the shadow desktop
// window from the start instead of waiting for a "show" command.
func spawnRdpHost(width, height int, viewer bool) (*RdpHost, error) {
	path := os.Getenv("AD_RDPHOST_BIN")
	if path == "" {
		exe, err := os.Executable()
		if err != nil {
			return nil, fmt.Errorf("resolve ad path: %w", err)
		}
		path = filepath.Join(filepath.Dir(exe), "ad-rdphost.exe")
	}
	if _, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf("ad-rdphost.exe not found at %s: %w", path, err)
	}
	cmd := exec.Command(path)
	// Drop any inherited AD_DISPLAY_* so a stale value can never win over
	// the dimensions requested on this open.
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "AD_DISPLAY_W=") || strings.HasPrefix(kv, "AD_DISPLAY_H=") ||
			strings.HasPrefix(kv, "AD_VIEWER=") {
			continue
		}
		cmd.Env = append(cmd.Env, kv)
	}
	if width > 0 {
		cmd.Env = append(cmd.Env, fmt.Sprintf("AD_DISPLAY_W=%d", width))
	}
	if height > 0 {
		cmd.Env = append(cmd.Env, fmt.Sprintf("AD_DISPLAY_H=%d", height))
	}
	if viewer {
		cmd.Env = append(cmd.Env, "AD_VIEWER=1")
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	el := &errLog{}
	cmd.Stderr = el
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start ad-rdphost: %w", err)
	}
	return &RdpHost{cmd: cmd, stdin: stdin, stdout: bufio.NewReader(stdout), errBuf: el}, nil
}

// WaitReady blocks until the helper reports the child session id or an
// error event.
func (h *RdpHost) WaitReady(timeout time.Duration) (int, error) {
	type res struct {
		line string
		err  error
	}
	ch := make(chan res, 1)
	go func() {
		line, err := h.stdout.ReadString('\n')
		ch <- res{line, err}
	}()
	var line string
	select {
	case r := <-ch:
		if r.err != nil {
			return 0, fmt.Errorf("ad-rdphost exited: %w (stderr: %s)", r.err, h.errBuf.String())
		}
		line = r.line
	case <-time.After(timeout):
		return 0, errors.New("timed out waiting for ad-rdphost ready event")
	}
	var ev rdpHostEvent
	if err := json.Unmarshal([]byte(line), &ev); err != nil {
		return 0, fmt.Errorf("unexpected ad-rdphost output %q", line)
	}
	switch ev.Event {
	case "ready":
		return ev.SessionID, nil
	case "error":
		return 0, fmt.Errorf("ad-rdphost: %s", ev.Message)
	default:
		return 0, fmt.Errorf("unexpected ad-rdphost event %q", ev.Event)
	}
}

// Close asks the helper to disconnect and exit, then force-kills it if
// it lingers.
func (h *RdpHost) Close() {
	if h == nil || h.cmd == nil || h.cmd.Process == nil {
		return
	}
	io.WriteString(h.stdin, "{\"cmd\":\"shutdown\"}\n")
	done := make(chan struct{})
	go func() { h.cmd.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		h.cmd.Process.Kill()
		<-done
	}
}
