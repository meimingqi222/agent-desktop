// Package session owns the facts ad can learn about Windows session
// infrastructure: whether TermService can host a shadow session,
// whether credentials can log on, and which sessions already exist.
package session

// Capability is what one probe run learned about the host.
type childProbe struct {
	ID     int
	Exists bool
	Owned  bool
}

type Capability struct {
	Platform            string   `json:"platform"`
	Edition             string   `json:"edition,omitempty"`
	TermServiceUp       bool     `json:"term_service_up"`
	ChildSessionOK      bool     `json:"child_session_supported"`
	MarkerHeld          bool     `json:"marker_held"`
	ExistingSession     int      `json:"existing_session_id,omitempty"`
	ForeignChildSession int      `json:"foreign_child_session_id,omitempty"`
	Supported           bool     `json:"supported"`
	Reasons             []string `json:"reasons,omitempty"`
}

// Probe gathers the facts that decide whether `surface open` can work.
// Every check is cheap and safe to run on each invocation, matching the
// design rule that probe runs before every create attempt.
func Probe() (*Capability, error) {
	return probe()
}
