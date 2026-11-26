//go:build !windows

package session

import "runtime"

func probe() (*Capability, error) {
	return &Capability{
		Platform:            runtime.GOOS,
		ExistingSession:     -1,
		ForeignChildSession: -1,
		Supported:           false,
		Reasons:             []string{"shadow sessions require Windows 10 1809+ / Windows 11"},
	}, nil
}
