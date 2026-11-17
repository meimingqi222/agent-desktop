//go:build windows

package session

import (
	"errors"
	"unsafe"
)

var (
	procWTSIsChildSessionsEnabled = wtsapi32.NewProc("WTSIsChildSessionsEnabled")
	procWTSEnableChildSessions    = wtsapi32.NewProc("WTSEnableChildSessions")
	procWTSGetChildSessionId      = wtsapi32.NewProc("WTSGetChildSessionId")
	procWTSLogoffSession          = wtsapi32.NewProc("WTSLogoffSession")
	procWTSDisconnectSession      = wtsapi32.NewProc("WTSDisconnectSession")
)

// ChildSessionsEnabled reports whether Windows child sessions are on.
func ChildSessionsEnabled() (bool, error) {
	var enabled uint32
	r, _, err := procWTSIsChildSessionsEnabled.Call(uintptr(unsafe.Pointer(&enabled)))
	if r == 0 {
		return false, err
	}
	return enabled != 0, nil
}

// EnableChildSessions turns child sessions on. It requires elevation
// and is idempotent.
func EnableChildSessions() error {
	r, _, err := procWTSEnableChildSessions.Call(1)
	if r == 0 {
		return err
	}
	return nil
}

const noChildSession = 0xFFFFFFFF

// ChildSessionID returns the id of the child session belonging to the
// caller's session, or 0 when none exists.
func ChildSessionID() (uint32, error) {
	var id uint32
	r, _, err := procWTSGetChildSessionId.Call(uintptr(unsafe.Pointer(&id)))
	if r == 0 {
		return 0, err
	}
	if id == noChildSession {
		return 0, nil
	}
	return id, nil
}

// DisconnectSession disconnects a WTS session without logging it off.
func DisconnectSession(sessionID uint32) error {
	r, _, err := procWTSDisconnectSession.Call(
		uintptr(wtsCurrentServerHandle), uintptr(sessionID), 0)
	if r == 0 {
		return err
	}
	return nil
}

// LogoffSession logs a WTS session off, tearing it down.
func LogoffSession(sessionID uint32) error {
	r, _, err := procWTSLogoffSession.Call(
		uintptr(wtsCurrentServerHandle), uintptr(sessionID), 1)
	if r == 0 {
		return err
	}
	return nil
}

// ErrNoChildSession reports that WTSGetChildSessionId found none.
var ErrNoChildSession = errors.New("no child session for the calling session")
