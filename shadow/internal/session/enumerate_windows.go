//go:build windows

package session

import (
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	wtsapi32                  = windows.NewLazySystemDLL("wtsapi32.dll")
	procWTSEnumerateSessionsW = wtsapi32.NewProc("WTSEnumerateSessionsW")
	procWTSFreeMemory         = wtsapi32.NewProc("WTSFreeMemory")
	procWTSQuerySessionInfoW  = wtsapi32.NewProc("WTSQuerySessionInformationW")
	kernel32                  = windows.NewLazySystemDLL("kernel32.dll")
	procProcessIdToSessionId  = kernel32.NewProc("ProcessIdToSessionId")
)

// WTSConnectState mirrors WTS_CONNECTSTATE_CLASS.
type WTSConnectState uint32

const (
	WTSActive       WTSConnectState = 0
	WTSConnected    WTSConnectState = 1
	WTSConnectQuery WTSConnectState = 2
	WTSShadow       WTSConnectState = 3
	WTSDisconnected WTSConnectState = 4
	WTSIdle         WTSConnectState = 5
	WTSListen       WTSConnectState = 6
	WTSReset        WTSConnectState = 7
	WTSDown         WTSConnectState = 8
	WTSInit         WTSConnectState = 9
)

// SessionInfo describes one interactive session visible to WTS.
type SessionInfo struct {
	ID       uint32 `json:"id"`
	Name     string `json:"name"`
	State    string `json:"state"`
	User     string `json:"user,omitempty"`
	IsShadow bool   `json:"is_shadow"`
}

const (
	wtsCurrentServerHandle = 0
	wtsUserName            = 5
)

type wtsSessionInfo struct {
	SessionID      uint32
	WinStationName *uint16
	State          WTSConnectState
}

// ListSessions enumerates interactive sessions through WTS. It is the
// real mechanism `surface open` uses to attach to an orphaned shadow
// session after a host crash.
func ListSessions() ([]SessionInfo, error) {
	var infos *wtsSessionInfo
	var count uint32
	r, _, err := procWTSEnumerateSessionsW.Call(
		uintptr(wtsCurrentServerHandle), 0, 1,
		uintptr(unsafe.Pointer(&infos)),
		uintptr(unsafe.Pointer(&count)))
	if r == 0 {
		return nil, err
	}
	defer procWTSFreeMemory.Call(uintptr(unsafe.Pointer(infos)))

	current, _ := currentSessionID()
	user, _ := currentUserName()

	raw := unsafe.Slice(infos, count)
	out := make([]SessionInfo, 0, len(raw))
	for _, si := range raw {
		s := SessionInfo{
			ID:    si.SessionID,
			State: connectStateName(si.State),
		}
		if si.WinStationName != nil {
			s.Name = windows.UTF16PtrToString(si.WinStationName)
		}
		if u, err := sessionUserName(si.SessionID); err == nil {
			s.User = u
		}
		s.IsShadow = si.SessionID != current && s.User != "" && s.User == user &&
			(si.State == WTSActive || si.State == WTSConnected || si.State == WTSDisconnected)
		out = append(out, s)
	}
	return out, nil
}

func sessionUserName(sessionID uint32) (string, error) {
	var buf *uint16
	var bytes uint32
	r, _, err := procWTSQuerySessionInfoW.Call(
		uintptr(wtsCurrentServerHandle), uintptr(sessionID), uintptr(wtsUserName),
		uintptr(unsafe.Pointer(&buf)), uintptr(unsafe.Pointer(&bytes)))
	if r == 0 {
		return "", err
	}
	defer procWTSFreeMemory.Call(uintptr(unsafe.Pointer(buf)))
	return windows.UTF16PtrToString(buf), nil
}

func currentSessionID() (uint32, error) {
	var id uint32
	r, _, err := procProcessIdToSessionId.Call(
		uintptr(os.Getpid()),
		uintptr(unsafe.Pointer(&id)))
	if r == 0 {
		return 0, err
	}
	return id, nil
}

func currentUserName() (string, error) {
	if u := os.Getenv("USERNAME"); u != "" {
		return u, nil
	}
	id, err := currentSessionID()
	if err != nil {
		return "", err
	}
	return sessionUserName(id)
}

func connectStateName(s WTSConnectState) string {
	switch s {
	case WTSActive:
		return "Active"
	case WTSConnected:
		return "Connected"
	case WTSConnectQuery:
		return "ConnectQuery"
	case WTSShadow:
		return "Shadow"
	case WTSDisconnected:
		return "Disconnected"
	case WTSIdle:
		return "Idle"
	case WTSListen:
		return "Listen"
	case WTSReset:
		return "Reset"
	case WTSDown:
		return "Down"
	case WTSInit:
		return "Init"
	default:
		return "Unknown"
	}
}

// findShadowSession reports the id of a candidate shadow session: a
// live WTS session owned by this user that is not our own.
func findShadowSession() (int, bool) {
	infos, err := ListSessions()
	if err != nil {
		return -1, false
	}
	for _, s := range infos {
		if s.IsShadow {
			return int(s.ID), true
		}
	}
	return -1, false
}
