//go:build !windows

package session

import "errors"

var errNoRunKey = errors.New("Run-key worker registration is Windows-only")

// RegisterWorkerRunKey is a no-op stub off Windows.
func RegisterWorkerRunKey(string) error { return errNoRunKey }

// UnregisterWorkerRunKey is a no-op stub off Windows.
func UnregisterWorkerRunKey() error { return nil }

// CurrentSessionID is unsupported off Windows.
func CurrentSessionID() (int, error) { return 0, errNoRunKey }
