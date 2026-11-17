//go:build !windows

package session

import "time"

// RdpHost is a no-op placeholder on non-Windows platforms.
type RdpHost struct{}

func spawnRdpHost() (*RdpHost, error) { return nil, ErrRPCUnavailable }

// WaitReady never succeeds on non-Windows platforms.
func (h *RdpHost) WaitReady(time.Duration) (int, error) { return 0, ErrRPCUnavailable }

// Close is a no-op on non-Windows platforms.
func (h *RdpHost) Close() {}
