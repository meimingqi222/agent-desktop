//go:build !windows

package session

// SessionInfo mirrors the Windows WTS session record so probe output
// keeps one shape across platforms.
type SessionInfo struct {
	ID       uint32 `json:"id"`
	Name     string `json:"name"`
	State    string `json:"state"`
	User     string `json:"user,omitempty"`
	IsShadow bool   `json:"is_shadow"`
}
