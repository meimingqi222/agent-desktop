//go:build !windows

package ipc

import (
	"errors"
	"net"
)

func dialPipe(pipeName string) (net.Conn, error) {
	return nil, errors.New("named pipes require Windows")
}

// ListenPipe is unavailable off Windows.
func ListenPipe(pipeName string) (net.Listener, error) {
	return nil, errors.New("named pipes require Windows")
}
