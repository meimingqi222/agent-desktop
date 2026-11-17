//go:build !windows

package session

// Marker is a no-op off Windows.
type Marker struct{}

// MarkerHeld always reports false off Windows.
func MarkerHeld() bool { return false }

// AcquireMarker always succeeds off Windows so callers can exercise
// the state machine during development on other platforms.
func AcquireMarker() (*Marker, bool, error) { return &Marker{}, true, nil }

// Release is a no-op off Windows.
func (m *Marker) Release() {}
