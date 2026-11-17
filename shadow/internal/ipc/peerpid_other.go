//go:build !windows

package ipc

import "net"

func clientPID(conn net.Conn) (uint32, error) {
	return 0, errUnsupportedPeer
}
