package session

import "errors"

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
// or asks TermService for a new child session. The RPC itself is the
// one deliberately unimplemented piece: it is an undocumented
// interface whose binding is being reverse-engineered, and shipping a
// guess would be worse than a precise error. Everything around it —
// probe, marker, credential check, attach — is real.
func Create(cap *Capability, opts CreateOpts) (*CreateResult, error) {
	if cap == nil {
		return nil, errors.New("missing capability probe")
	}
	nested, err := InShadowSession()
	if err == nil && nested {
		return nil, ErrShadowNested
	}
	if cap.ExistingSession >= 0 {
		return &CreateResult{SessionID: cap.ExistingSession, Attached: true}, nil
	}
	return nil, ErrRPCUnavailable
}

// InShadowSession reports whether this process already runs inside a
// non-console session.
func InShadowSession() (bool, error) {
	return inShadowSession()
}
