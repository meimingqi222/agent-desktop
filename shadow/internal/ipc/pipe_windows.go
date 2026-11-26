//go:build windows

package ipc

import (
	"fmt"
	"net"
	"strings"

	"github.com/Microsoft/go-winio"
	"golang.org/x/sys/windows"
)

// pipePath normalizes a pipe argument: a bare name becomes the full
// \\.\pipe\ path; an already-prefixed path passes through.
func pipePath(name string) string {
	if strings.HasPrefix(name, `\\`) {
		return name
	}
	return `\\.\pipe\` + name
}

// dialPipe connects to the worker's named pipe as a client.
func dialPipe(pipeName string) (net.Conn, error) {
	conn, err := winio.DialPipe(pipePath(pipeName), nil)
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", pipeName, err)
	}
	return conn, nil
}

// ListenPipe creates the in-session server end. The ACL restricts the
// pipe to the owning user; combined with the client-PID check in Serve
// this is the IPC trust boundary.
func ListenPipe(pipeName string) (net.Listener, error) {
	descriptor, err := currentUserPipeSecurityDescriptor()
	if err != nil {
		return nil, fmt.Errorf("listen %s: %w", pipeName, err)
	}
	l, err := winio.ListenPipe(pipePath(pipeName), &winio.PipeConfig{SecurityDescriptor: descriptor})
	if err != nil {
		return nil, fmt.Errorf("listen %s: %w", pipeName, err)
	}
	return l, nil
}

func currentUserPipeSecurityDescriptor() (string, error) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return "", fmt.Errorf("query current user token: %w", err)
	}
	sid := user.User.Sid.String()
	if sid == "" {
		return "", fmt.Errorf("query current user token: empty user SID")
	}
	return fmt.Sprintf("D:P(A;;GA;;;%s)(A;;GA;;;SY)", sid), nil
}
