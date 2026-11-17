//go:build !windows

package session

// ListSessions is a no-op off Windows; WTS does not exist there.
func ListSessions() ([]SessionInfo, error) {
	return nil, nil
}

func findShadowSession() (int, bool) {
	return -1, false
}
