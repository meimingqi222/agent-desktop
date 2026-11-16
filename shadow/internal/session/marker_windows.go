//go:build windows

package session

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

// markerName is the global mutex whose presence means an ad host owns
// a live shadow session, matching the marker-mutex reuse strategy.
const markerName = `Global\ad-shadow-session`

var (
	procCreateMutexW = kernel32.NewProc("CreateMutexW")
	procOpenMutexW   = kernel32.NewProc("OpenMutexW")
	procReleaseMutex = kernel32.NewProc("ReleaseMutex")
)

const synchronizeAccess = 0x00100000

// Marker is the held mutex. Release it to free the claim.
type Marker struct {
	handle windows.Handle
}

// MarkerHeld reports whether some live process holds the shadow marker.
func MarkerHeld() bool {
	name, err := windows.UTF16PtrFromString(markerName)
	if err != nil {
		return false
	}
	h, _, err := procOpenMutexW.Call(
		uintptr(synchronizeAccess), 0, uintptr(unsafe.Pointer(name)))
	if h == 0 {
		return false
	}
	windows.CloseHandle(windows.Handle(h))
	_ = err
	return true
}

// AcquireMarker takes the global shadow-session marker. The second
// return value is false when another live host already holds it.
func AcquireMarker() (*Marker, bool, error) {
	name, err := windows.UTF16PtrFromString(markerName)
	if err != nil {
		return nil, false, err
	}
	h, _, callErr := procCreateMutexW.Call(0, 0, uintptr(unsafe.Pointer(name)))
	if h == 0 {
		return nil, false, callErr
	}
	m := &Marker{handle: windows.Handle(h)}
	if callErr == windows.ERROR_ALREADY_EXISTS {
		return m, false, nil
	}
	return m, true, nil
}

// Release drops the marker handle.
func (m *Marker) Release() {
	if m == nil || m.handle == 0 {
		return
	}
	windows.CloseHandle(m.handle)
	m.handle = 0
}
