// Package rdp will own the RDP loopback into the shadow session:
// IMsRdpClient lifecycle for the optional viewer and IMsRdpInputSink
// for physical (non-UIA) input into the shadow desktop.
//
// The channel is a deliberate seam: physical input into the shadow
// session is what makes it invisible to the user's real desktop, and
// it lands with the connect step of the lifecycle. Until then every
// entry point returns ErrNotBound.
package rdp

import "errors"

// ErrNotBound reports that the RDP channel is not wired yet.
var ErrNotBound = errors.New("rdp loopback not implemented")

// InputSink is the IMsRdpInputSink equivalent: physical mouse and
// keyboard events targeted at the shadow session's input stack.
type InputSink interface {
	SendMouseMove(x, y int) error
	SendMouseClick(button int, down bool) error
	SendMouseWheel(delta int) error
	SendKeyboardEvent(scanCode uint16, down bool) error
	SendKeyStroke(text string) error
}
