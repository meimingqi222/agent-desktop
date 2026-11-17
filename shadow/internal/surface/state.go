// Package surface owns the shadow-session lifecycle state machine
// (Idle through Ready to Destroy) and the manager adapters talk to.
package surface

// State is one step of the shadow-session lifecycle.
type State string

const (
	StateIdle       State = "idle"
	StateProbing    State = "probing"
	StateCredential State = "credential_precheck"
	StateCreating   State = "creating"
	StateConnecting State = "connecting"
	StateInjecting  State = "injecting"
	StateReady      State = "ready"
	StateDestroying State = "destroying"
	StateFailed     State = "failed"
)

// StateError is the structured failure adapters translate into
// envelope errors.
type StateError struct {
	Code       string `json:"code"`
	Message    string `json:"message"`
	Suggestion string `json:"suggestion,omitempty"`
	State      State  `json:"state"`
}

func (e *StateError) Error() string { return e.Message }

func stateError(state State, code, message, suggestion string) *StateError {
	return &StateError{Code: code, Message: message, Suggestion: suggestion, State: state}
}
