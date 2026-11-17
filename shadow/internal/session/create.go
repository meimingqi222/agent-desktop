package session

import (
	"errors"
	"sync"
	"time"
)

// ErrShadowNested mirrors the platform rule that a shadow session
// cannot be created from inside a shadow session.
var ErrShadowNested = errors.New("a shadow session cannot be created from a shadow session")

// ErrRPCUnavailable reports that the TermService child-session RPC
// binding is not present in this build.
var ErrRPCUnavailable = errors.New("TermService child-session RPC binding not implemented")

// CreateOpts carries what a create call needs once the RPC lands.
type CreateOpts struct {
	Username string
	Domain   string
	Password string
	Width    int
	Height   int
}

// CreateResult describes the session a create attempt produced.
type CreateResult struct {
	SessionID int  `json:"session_id"`
	Attached  bool `json:"attached"`
}

// Create either attaches to a live shadow session found by the probe
// or spawns a new WTS child session via the ad-rdphost helper, which
// hosts the mstscax control with ConnectToChildSession. The child
// session stays connected only while the helper lives; the returned
// result records its session id and the host is tracked for Close.
func Create(cap *Capability, opts CreateOpts) (*CreateResult, error) {
	if cap == nil {
		return nil, errors.New("missing capability probe")
	}
	nested, err := InShadowSession()
	if err == nil && nested {
		return nil, ErrShadowNested
	}
	if cap.ExistingSession >= 0 {
		// Reconnect only when the session is disconnected: an Active
		// child is already anchored by the earlier ad-rdphost, and
		// stacking another RDP client gains nothing.
		if shadowDisconnected(cap.ExistingSession) {
			spawnAttachHost(0, 0)
		}
		return &CreateResult{SessionID: cap.ExistingSession, Attached: true}, nil
	}
	host, err := spawnRdpHost(opts.Width, opts.Height)
	if err != nil {
		return nil, err
	}
	id, err := host.WaitReady(rdpReadyTimeout)
	if err != nil {
		host.Close()
		return nil, err
	}
	currentHostMu.Lock()
	currentHost = host
	currentHostMu.Unlock()
	return &CreateResult{SessionID: id, Attached: false}, nil
}

// shadowDisconnected reports whether the given session id is a
// disconnected (but still logged-on) session that needs an RDP
// reconnect.
func shadowDisconnected(sessionID int) bool {
	infos, err := ListSessions()
	if err != nil {
		return false
	}
	for _, s := range infos {
		if int(s.ID) == sessionID {
			return s.State != "Active"
		}
	}
	return false
}

// spawnAttachHost starts an ad-rdphost to re-anchor a disconnected
// child session and tracks it for CloseSession.
func spawnAttachHost(width, height int) {
	host, err := spawnRdpHost(width, height)
	if err != nil {
		return
	}
	if _, err := host.WaitReady(attachReadyTimeout); err != nil {
		host.Close()
		return
	}
	currentHostMu.Lock()
	currentHost = host
	currentHostMu.Unlock()
}

var (
	currentHostMu sync.Mutex
	currentHost   *RdpHost
)

const (
	rdpReadyTimeout    = 90 * time.Second
	attachReadyTimeout = 30 * time.Second
)

// CloseSession disconnects the live child session and stops the
// helper. Safe to call when no session was created.
func CloseSession() {
	currentHostMu.Lock()
	h := currentHost
	currentHost = nil
	currentHostMu.Unlock()
	h.Close()
}

// InShadowSession reports whether this process already runs inside a
// non-console session.
func InShadowSession() (bool, error) {
	return inShadowSession()
}
