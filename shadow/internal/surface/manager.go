package surface

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/meimingqi222/agent-desktop-shadow/internal/ipc"
	"github.com/meimingqi222/agent-desktop-shadow/internal/session"
)

const (
	codePlatformNotSupported   = "PLATFORM_NOT_SUPPORTED"
	codeTermServiceUnavailable = "TERM_SERVICE_UNAVAILABLE"
	codeSurfaceNotReady        = "SURFACE_NOT_READY"
	codeCredentialRejected     = "CREDENTIAL_REJECTED"
	codeAuthRequired           = "AUTH_REQUIRED"
)

// OpenOpts controls one `surface open` run.
type OpenOpts struct {
	Username string
	Domain   string
	Password string
	Width    int
	Height   int
	Viewer   bool
	// AgentDesktopBin is the --agent-desktop-bin override so the
	// in-session worker finds the same binary the host uses.
	AgentDesktopBin string
}

// Info reports what a successful Open produced.
type Info struct {
	SessionID int    `json:"session_id"`
	Attached  bool   `json:"attached"`
	State     State  `json:"state"`
	Surface   string `json:"surface"`
	PipeName  string `json:"pipe_name,omitempty"`
}

// Status is the manager's externally visible snapshot.
type Status struct {
	State     State               `json:"state"`
	SessionID int                 `json:"session_id,omitempty"`
	Cap       *session.Capability `json:"capability,omitempty"`
	LastError string              `json:"last_error,omitempty"`
}

// Manager drives the lifecycle: probe → credential → create →
// connect → inject → ready. Steps whose platform pieces do not exist
// yet fail closed with precise errors instead of pretending.
type Manager struct {
	mu       sync.Mutex
	state    State
	cap      *session.Capability
	marker   *session.Marker
	sessID   int
	lastErr  string
	pipeName string
	client   *ipc.Client
	attached bool
}

// NewManager builds a manager in Idle.
func NewManager() *Manager {
	return &Manager{state: StateIdle, sessID: -1}
}

// Current returns the state machine's snapshot.
func (m *Manager) Current() Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	st := Status{State: m.state, Cap: m.cap, LastError: m.lastErr}
	if m.sessID >= 0 {
		st.SessionID = m.sessID
	}
	return st
}

// RequireReady is the gate every pip command passes through.
func (m *Manager) RequireReady() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.state == StateReady {
		return nil
	}
	msg := fmt.Sprintf("shadow surface is %s, not ready", m.state)
	if m.lastErr != "" {
		msg = fmt.Sprintf("%s: %s", msg, m.lastErr)
	}
	return stateError(m.state, codeSurfaceNotReady, msg,
		"run `ad surface open` first, or pass --surface main to act on the primary desktop")
}

// EnsureReady re-attaches a later `ad` process to a shadow session an
// earlier invocation left running, and re-dials the worker when the
// pipe broke. It only attaches — it never creates a session on its own.
func (m *Manager) EnsureReady(ctx context.Context) error {
	m.mu.Lock()
	ready := m.state == StateReady
	broken := ready && (m.client == nil || m.client.Broken())
	pipeName := m.pipeName
	m.mu.Unlock()
	if ready && !broken {
		return nil
	}
	if broken {
		// The pipe died but the session is still alive; re-dial the
		// in-session worker on the same pipe name.
		m.setState(StateConnecting)
		client, err := waitWorker(pipeName, workerRetry)
		if err != nil {
			return m.RequireReady()
		}
		m.mu.Lock()
		m.client = client
		m.setStateLocked(StateReady)
		m.mu.Unlock()
		return nil
	}
	m.mu.Lock()
	idle := m.state == StateIdle
	m.mu.Unlock()
	if !idle {
		return m.RequireReady()
	}
	cap, err := session.Probe()
	if err != nil || cap == nil || cap.ExistingSession < 0 {
		return m.RequireReady()
	}
	if _, err := m.Open(ctx, OpenOpts{}); err != nil {
		return m.RequireReady()
	}
	return m.RequireReady()
}

// Open runs the lifecycle up to Ready or the first hard failure.
func (m *Manager) Open(ctx context.Context, opts OpenOpts) (*Info, error) {
	m.mu.Lock()
	if m.state == StateReady {
		info := &Info{SessionID: m.sessID, State: m.state, Surface: "pip", PipeName: m.pipeName}
		m.mu.Unlock()
		return info, nil
	}
	m.setStateLocked(StateProbing)
	m.mu.Unlock()

	cap, err := session.Probe()
	if err != nil {
		return nil, m.fail(StateProbing, "INTERNAL", fmt.Sprintf("probe failed: %v", err), "")
	}
	m.mu.Lock()
	m.cap = cap
	m.mu.Unlock()
	if !cap.Supported {
		code := codeTermServiceUnavailable
		if !cap.ChildSessionOK {
			code = codePlatformNotSupported
		}
		return nil, m.fail(StateProbing, code,
			fmt.Sprintf("shadow session unsupported: %s", joinReasons(cap.Reasons)),
			"commands continue to work with --surface main")
	}

	m.setState(StateCredential)
	// WTS child sessions auto-log on as the parent session's user, so a
	// password is optional; when supplied it is still verified up front.
	if opts.Password != "" {
		domain := opts.Domain
		if err := session.CheckCredential(opts.Username, domain, opts.Password); err != nil {
			return nil, m.fail(StateCredential, codeCredentialRejected,
				fmt.Sprintf("credential check failed for %s: %v", opts.Username, err),
				"verify the Windows account password; Microsoft accounts use the full email as username")
		}
	}

	m.setState(StateCreating)
	marker, acquired, err := session.AcquireMarker()
	if err != nil {
		return nil, m.fail(StateCreating, "INTERNAL", fmt.Sprintf("marker acquire failed: %v", err), "")
	}
	if !acquired && cap.ExistingSession < 0 {
		marker.Release()
		return nil, m.fail(StateCreating, codeTermServiceUnavailable,
			"another ad host holds the shadow-session marker but no live session was found",
			"close the other host or run `ad surface close` there first")
	}
	m.mu.Lock()
	m.marker = marker
	m.mu.Unlock()
	// Register the in-session worker before creating the session: the
	// child session auto-logs on as the same user, so the Run key fires
	// there on first logon.
	if cap.ExistingSession < 0 {
		if werr := registerWorker(opts.AgentDesktopBin); werr != nil {
			marker.Release()
			return nil, m.fail(StateCreating, "INTERNAL",
				fmt.Sprintf("worker registration failed: %v", werr), "")
		}
	}
	res, err := session.Create(cap, session.CreateOpts{
		Username: opts.Username,
		Domain:   opts.Domain,
		Password: opts.Password,
		Width:    opts.Width,
		Height:   opts.Height,
	})
	if err != nil {
		marker.Release()
		switch {
		case errors.Is(err, session.ErrShadowNested):
			return nil, m.fail(StateCreating, codePlatformNotSupported, err.Error(),
				"run ad from the primary (console) session, not from inside a shadow session")
		case errors.Is(err, session.ErrRPCUnavailable):
			return nil, m.fail(StateCreating, codeTermServiceUnavailable,
				"shadow-session creation RPC is not bound in this build",
				"probe, marker, credential check and attach are live; the TermService "+
					"child-session RPC seam lands with v0.3 — use --surface main meanwhile")
		default:
			return nil, m.fail(StateCreating, codeTermServiceUnavailable, err.Error(), "")
		}
	}

	m.mu.Lock()
	m.sessID = res.SessionID
	m.pipeName = pipeNameFor(res.SessionID)
	m.attached = res.Attached
	m.mu.Unlock()

	m.setState(StateConnecting)
	// Wait for the in-session worker: the Run key fires on the child
	// session's first logon, then ad-remote opens the pipe.
	client, err := waitWorker(m.pipeName, workerWait)
	if err != nil {
		m.closeLocked()
		return nil, m.fail(StateConnecting, codeTermServiceUnavailable,
			fmt.Sprintf("shadow session %d created but the in-session worker did not come up: %v", res.SessionID, err),
			"check that ad-remote.exe sits next to ad.exe; retry `ad surface open`")
	}
	m.mu.Lock()
	m.client = client
	m.setStateLocked(StateReady)
	info := &Info{SessionID: m.sessID, Attached: m.attached, State: m.state, Surface: "pip", PipeName: m.pipeName}
	m.mu.Unlock()
	return info, nil
}

// Forward sends one command to the in-session worker over IPC. It is
// only reachable once the state machine is Ready.
func (m *Manager) Forward(ctx context.Context, command string, args []string, stdin []byte) ([]byte, error) {
	m.mu.Lock()
	c := m.client
	m.mu.Unlock()
	if c == nil {
		return nil, errors.New("no IPC client: worker handshake has not completed")
	}
	resp, err := c.Call(ctx, command, args, stdin)
	if err != nil {
		return nil, err
	}
	return resp.Payload, nil
}

// Close tears the session down and releases the marker.
func (m *Manager) Close(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.setStateLocked(StateDestroying)
	err := m.closeLocked()
	m.setStateLocked(StateIdle)
	if err != nil {
		m.lastErr = err.Error()
		return err
	}
	return nil
}

// closeLocked tears down session resources. The caller must hold m.mu
// or be on a path that has not published state yet. It returns the
// first teardown error, if any; callers on a failure path may ignore it.
func (m *Manager) closeLocked() error {
	if m.client != nil {
		m.client.Close()
		m.client = nil
	}
	session.UnregisterWorkerRunKey()
	session.CloseSession()
	sessID := m.sessID
	if sessID < 0 {
		// This ad process never opened a session, but an earlier one
		// may have left a live shadow session behind.
		if cap, err := session.Probe(); err == nil && cap != nil {
			sessID = cap.ExistingSession
		}
	}
	var firstErr error
	if sessID >= 0 && !m.attached {
		// Only log off a session we actually own: it must be the child
		// session of THIS session. Probing alone is not enough — an
		// unrelated same-user RDP session must never be logged off.
		if childID, err := session.ChildSessionID(); err == nil && int(childID) == sessID {
			if err := session.LogoffSession(uint32(sessID)); err != nil {
				firstErr = fmt.Errorf("logoff session %d: %w", sessID, err)
			}
		}
	}
	if m.marker != nil {
		m.marker.Release()
		m.marker = nil
	}
	m.sessID = -1
	m.pipeName = ""
	m.attached = false
	return firstErr
}

func (m *Manager) setState(s State) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.setStateLocked(s)
}

func (m *Manager) setStateLocked(s State) {
	m.state = s
}

func (m *Manager) fail(at State, code, message, suggestion string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.state = at
	m.lastErr = message
	return stateError(at, code, message, suggestion)
}

func joinReasons(r []string) string {
	out := ""
	for i, s := range r {
		if i > 0 {
			out += "; "
		}
		out += s
	}
	return out
}

func pipeNameFor(sessionID int) string {
	return fmt.Sprintf(`ad-pip-%d`, sessionID)
}

// Response is the wire shape the IPC client returns; defined here to
// keep ipc free of surface concerns. Kept for future multi-command
// fan-out.
type Response struct {
	Payload json.RawMessage
}

const (
	workerWait  = 60 * time.Second
	workerRetry = 15 * time.Second
)

// registerWorker puts ad-remote.exe (expected next to ad.exe) on the
// current user's Run key with --child so it only serves inside the
// shadow session.
func registerWorker(bin string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	remote := filepath.Join(filepath.Dir(exe), "ad-remote.exe")
	if _, err := os.Stat(remote); err != nil {
		return fmt.Errorf("ad-remote.exe not found at %s", remote)
	}
	// No --host-pid: ad invocations are separate processes and a later
	// `ad` must be able to re-attach to a worker launched for an
	// earlier one.
	args := fmt.Sprintf(`"%s" --child`, remote)
	if bin == "" {
		bin = os.Getenv("AD_AGENT_DESKTOP_BIN")
	}
	if bin != "" {
		// The worker starts at logon in a different working directory, so
		// a relative path must be made absolute before it is stored.
		if abs, err := filepath.Abs(bin); err == nil {
			bin = abs
		}
		args += fmt.Sprintf(` --agent-desktop-bin "%s"`, bin)
	}
	return session.RegisterWorkerRunKey(args)
}

// waitWorker polls the worker pipe until the in-session ad-remote
// answers or the timeout expires.
func waitWorker(pipeName string, timeout time.Duration) (*ipc.Client, error) {
	deadline := time.Now().Add(timeout)
	var lastErr error
	for time.Now().Before(deadline) {
		c, err := ipc.Dial(pipeName)
		if err == nil {
			return c, nil
		}
		lastErr = err
		time.Sleep(500 * time.Millisecond)
	}
	return nil, fmt.Errorf("no worker on %s after %s: %v", pipeName, timeout, lastErr)
}
