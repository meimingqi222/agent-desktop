//go:build !windows

package session

func probeChildSession() childProbe {
	return childProbe{ID: -1}
}

// OwnedChildSession is always false off Windows.
func OwnedChildSession(int) bool { return false }

// PipePresent is always false off Windows.
func PipePresent(string) bool { return false }

func recordChildOwnership(int) error { return nil }

func ClearChildOwnership() error { return nil }
