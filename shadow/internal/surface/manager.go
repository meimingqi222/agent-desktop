package surface

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

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
	if opts.Password == "" {
		return nil, m.fail(StateCredential, codeAuthRequired,
			"a Windows account credential is required to create the shadow session",
			"pass --username/--password, or set AD_SHADOW_USERNAME/AD_SHADOW_PASSWORD; "+
				"pure Windows Hello/PIN accounts need a password first")
	}
	domain := opts.Domain
	if err := session.CheckCredential(opts.Username, domain, opts.Password); err != nil {
		return nil, m.fail(StateCredential, codeCredentialRejected,
			fmt.Sprintf("credential check failed for %s: %v", opts.Username, err),
			"verify the Windows account password; Microsoft accounts use the full email as username")
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
	res, err := session.Create(cap, session.CreateOpts{
		Username: opts.Username,
		Domain:   domain,
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
	m.mu.Unlock()

	if res.Attached {
		return nil, m.fail(StateConnecting, codeTermServiceUnavailable,
			fmt.Sprintf("found live shadow session %d but the in-session worker handshake is not implemented", res.SessionID),
			"attach lands with v0.3 — `ad surface close` releases the marker")
	}

	m.setState(StateConnecting)
	return nil, m.fail(StateConnecting, codeTermServiceUnavailable,
		"session created but RDP loopback connect is not implemented",
		"the RDP loopback seam lands with v0.4 — use --surface main meanwhile")
}

// Forward sends one command to the in-session worker over IPC. It is
// only reachable once the state machine is Ready.
func (m *Manager) Forward(ctx context.Context, command string, args []string) ([]byte, error) {
	m.mu.Lock()
	c := m.client
	m.mu.Unlock()
	if c == nil {
		return nil, errors.New("no IPC client: worker handshake has not completed")
	}
	resp, err := c.Call(ctx, command, args)
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
	if m.client != nil {
		m.client.Close()
		m.client = nil
	}
	if m.marker != nil {
		m.marker.Release()
		m.marker = nil
	}
	m.sessID = -1
	m.pipeName = ""
	m.setStateLocked(StateIdle)
	return nil
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
