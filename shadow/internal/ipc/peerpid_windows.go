//go:build windows

package ipc

import (
	"net"
	"unsafe"

	"golang.org/x/sys/windows"
)

var procGetNamedPipeClientProcessId = kernel32Pipe.NewProc("GetNamedPipeClientProcessId")

var kernel32Pipe = windows.NewLazySystemDLL("kernel32.dll")

// clientPID returns the PID of the process on the other end of a named
// pipe connection, per the verify-the-host trust rule.
func clientPID(conn net.Conn) (uint32, error) {
	type pipeHaver interface{ Fd() uintptr }
	ph, ok := conn.(pipeHaver)
	if !ok {
		return 0, errUnsupportedPeer
	}
	var pid uint32
	r, _, err := procGetNamedPipeClientProcessId.Call(
		ph.Fd(), uintptr(unsafe.Pointer(&pid)))
	if r == 0 {
		return 0, err
	}
	return pid, nil
}
