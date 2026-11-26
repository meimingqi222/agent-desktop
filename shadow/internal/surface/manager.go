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
	"github.com/meimingqi222/agent-desktop-shadow/internal/remote"
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
// connect → inject → ready.
type Manager struct {
	mu              sync.Mutex
	state           State
	cap             *session.Capability
	sessID          int
	lastErr         string
	pipeName        string
	client          *ipc.Client
	attached        bool
	agentDesktopBin string
}

// NewManager builds a manager in Idle.
func NewManager() *Manager {
	return &Manager{state: StateIdle, sessID: -1}
}

// SetAgentDesktopBin records the binary path later automatic reattach
// calls must preserve for the in-session worker.
func (m *Manager) SetAgentDesktopBin(bin string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.agentDesktopBin = bin
}

// Current returns the state machine's snapshot. Because every CLI
// invocation is a fresh process, an Idle manager first reconciles with
// a shadow session an earlier invocation left running.
func (m *Manager) Current() Status {
	m.refreshExternalState()
	m.mu.Lock()
	defer m.mu.Unlock()
	st := Status{State: m.state, Cap: m.cap, LastError: m.lastErr}
	if m.sessID >= 0 {
		st.SessionID = m.sessID
	}
	return st
}

func (m *Manager) reattachOpenOpts() OpenOpts {
	m.mu.Lock()
	defer m.mu.Unlock()
	return OpenOpts{AgentDesktopBin: m.agentDesktopBin}
}

func (m *Manager) openOptsWithDefaults(opts OpenOpts) OpenOpts {
	if opts.AgentDesktopBin != "" {
		return opts
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	opts.AgentDesktopBin = m.agentDesktopBin
	return opts
}

func (m *Manager) refreshExternalState() {
	m.mu.Lock()
	shouldProbe := m.state == StateIdle
	m.mu.Unlock()
	if !shouldProbe {
		return
	}
	cap, err := session.Probe()
	if err != nil || cap == nil || cap.ExistingSession < 0 {
		return
	}
	pipeName := pipeNameFor(cap.ExistingSession)
	state := StateConnecting
	if session.PipePresent(pipeName) {
		state = StateReady
	}
	m.mu.Lock()
	if m.state == StateIdle {
		m.cap = cap
		m.sessID = cap.ExistingSession
		m.pipeName = pipeName
		m.state = state
	}
	m.mu.Unlock()
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
	sessID := m.sessID
	m.mu.Unlock()
	if ready && !broken {
		return nil
	}
	if broken {
		m.setState(StateConnecting)
		client, err := waitWorker(pipeName, workerRetry)
		if err != nil && sessID >= 0 && !session.PipePresent(pipeName) {
			_, _ = launchWorkerInSession(sessID, m.reattachOpenOpts().AgentDesktopBin)
			client, err = waitWorker(pipeName, workerRetry)
		}
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
	if _, err := m.Open(ctx, m.reattachOpenOpts()); err != nil {
		return m.RequireReady()
	}
	return m.RequireReady()
}

// Open runs the lifecycle up to Ready or the first hard failure.
func (m *Manager) Open(ctx context.Context, opts OpenOpts) (*Info, error) {
	opts = m.openOptsWithDefaults(opts)
	m.mu.Lock()
	if m.state == StateReady && m.client != nil {
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
	if cap.ForeignChildSession >= 0 && cap.ExistingSession < 0 {
		return nil, m.fail(StateProbing, codeTermServiceUnavailable,
			fmt.Sprintf("child session %d already exists but was not created by ad", cap.ForeignChildSession),
			"close that child session before opening an ad shadow session; ad never hijacks a foreign child session")
	}

	m.setState(StateCredential)
	// WTS child sessions auto-log on as the parent session's user, so a
	// password is optional; when supplied it is still verified up front.
	if opts.Password != "" {
		if err := session.CheckCredential(opts.Username, opts.Domain, opts.Password); err != nil {
			return nil, m.fail(StateCredential, codeCredentialRejected,
				fmt.Sprintf("credential check failed for %s: %v", opts.Username, err),
				"verify the Windows account password; Microsoft accounts use the full email as username")
		}
	}

	m.setState(StateCreating)
	// Register the in-session worker before creating or attaching. The
	// Run key fires on a child session's first logon; on later logons it
	// is also the recovery path that starts the supervisor.
	if werr := registerWorker(opts.AgentDesktopBin); werr != nil {
		return nil, m.fail(StateCreating, "INTERNAL",
			fmt.Sprintf("worker registration failed: %v", werr), "")
	}
	res, err := session.Create(cap, session.CreateOpts{
		Username: opts.Username,
		Domain:   opts.Domain,
		Password: opts.Password,
		Width:    opts.Width,
		Height:   opts.Height,
		Viewer:   opts.Viewer,
	})
	if err != nil {
		if errors.Is(err, session.ErrShadowNested) {
			return nil, m.fail(StateCreating, codePlatformNotSupported, err.Error(),
				"run ad from the parent session, not from inside a shadow session")
		}
		return nil, m.fail(StateCreating, codeTermServiceUnavailable, err.Error(), "")
	}

	m.mu.Lock()
	m.sessID = res.SessionID
	m.pipeName = pipeNameFor(res.SessionID)
	m.attached = res.Attached
	m.mu.Unlock()

	m.setState(StateConnecting)
	launchNote := ""
	if res.Attached && !session.PipePresent(m.pipeName) {
		m.setState(StateInjecting)
		if pid, lerr := launchWorkerInSession(res.SessionID, opts.AgentDesktopBin); lerr == nil {
			launchNote = fmt.Sprintf("; launched worker supervisor as pid %d", pid)
		} else {
			launchNote = fmt.Sprintf("; direct worker launch was unavailable: %v", lerr)
		}
		m.setState(StateConnecting)
	}
	// Wait for the in-session worker: the Run key fires on the child
	// session's first logon, then the supervisor starts the pipe worker.
	client, err := waitWorker(m.pipeName, workerWait)
	if err != nil {
		_ = m.closeLocked()
		verb := "created"
		if res.Attached {
			verb = "attached"
		}
		return nil, m.fail(StateConnecting, codeTermServiceUnavailable,
			fmt.Sprintf("shadow session %d %s but the in-session worker did not come up: %v%s", res.SessionID, verb, err, launchNote),
			"the unrecoverable owned session was torn down; check that ad-remote.exe sits next to ad.exe, then run `ad surface open` again")
	}
	m.mu.Lock()
	m.client = client
	m.setStateLocked(StateReady)
	info := &Info{SessionID: m.sessID, Attached: m.attached, State: m.state, Surface: "pip", PipeName: m.pipeName}
	m.mu.Unlock()
	if opts.Viewer && res.Attached {
		// A fresh spawn already shows the window via AD_VIEWER; an
		// attach keeps the earlier ad-rdphost, whose window is
		// toggled through its control pipe.
		if verr := session.ViewerCommand("show"); verr != nil {
			m.mu.Lock()
			m.lastErr = fmt.Sprintf("viewer show failed: %v", verr)
			m.mu.Unlock()
		}
	}
	return info, nil
}

// Viewer toggles the shadow-desktop window ad-rdphost renders on the
// parent session. It requires a live shadow session and attaches to
// one left running by an earlier `ad` when needed.
func (m *Manager) Viewer(ctx context.Context, show bool) error {
	if err := m.EnsureReady(ctx); err != nil {
		return err
	}
	cmd := "hide"
	if show {
		cmd = "show"
	}
	if err := session.ViewerCommand(cmd); err != nil {
		return stateError(StateReady, codeSurfaceNotReady,
			fmt.Sprintf("viewer %s failed: %v", cmd, err),
			"the ad-rdphost helper may be gone; run `ad surface open` again")
	}
	return nil
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

// Close tears the session down.
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
	sessID := m.sessID
	if sessID < 0 {
		// This ad process never opened a session, but an earlier one
		// may have left a live shadow session behind.
		if cap, err := session.Probe(); err == nil && cap != nil {
			sessID = cap.ExistingSession
		}
	}
	owned := sessID >= 0 && session.OwnedChildSession(sessID)
	session.UnregisterWorkerRunKey()
	session.CloseSession()
	var firstErr error
	if owned {
		if err := session.LogoffSession(uint32(sessID)); err != nil {
			firstErr = fmt.Errorf("logoff session %d: %w", sessID, err)
		} else {
			_ = session.ClearChildOwnership()
		}
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
	m.state = StateFailed
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
// current user's Run key with --child so it only runs inside the
// shadow session.
func registerWorker(bin string) error {
	remotePath, bin, err := workerLaunchSpec(bin)
	if err != nil {
		return err
	}
	args := fmt.Sprintf(`"%s" --child`, remotePath)
	if bin != "" {
		args += fmt.Sprintf(` --agent-desktop-bin "%s"`, bin)
	}
	return session.RegisterWorkerRunKey(args)
}

func workerLaunchSpec(bin string) (string, string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", "", err
	}
	remotePath := filepath.Join(filepath.Dir(exe), "ad-remote.exe")
	if _, err := os.Stat(remotePath); err != nil {
		return "", "", fmt.Errorf("ad-remote.exe not found at %s", remotePath)
	}
	if bin == "" {
		bin = os.Getenv("AD_AGENT_DESKTOP_BIN")
	}
	if bin != "" {
		if abs, err := filepath.Abs(bin); err == nil {
			bin = abs
		}
	}
	return remotePath, bin, nil
}

// launchWorkerInSession is a best-effort cold start for an existing
// child session. It uses documented WTS token process creation and is
// normally available only to privileged callers; unprivileged setups
// recover through the in-session supervisor instead.
func launchWorkerInSession(sessionID int, bin string) (int, error) {
	remotePath, bin, err := workerLaunchSpec(bin)
	if err != nil {
		return 0, err
	}
	args := []string{"--child"}
	if bin != "" {
		args = append(args, "--agent-desktop-bin", bin)
	}
	return remote.LaunchInSession(remote.LaunchOpts{
		SessionID:  sessionID,
		Executable: remotePath,
		Args:       args,
		WorkingDir: filepath.Dir(remotePath),
	})
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
