//go:build windows

package session

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	advapi32        = windows.NewLazySystemDLL("advapi32.dll")
	procLogonUserW  = advapi32.NewProc("LogonUserW")
	procGetUserName = advapi32.NewProc("GetUserNameW")
)

const (
	logon32LogonInteractive = 2
	logon32ProviderDefault  = 0
)

// CheckCredential validates a username/password pair with LogonUser
// without creating any session. The password is used only in memory
// and never persisted, matching the credential-handling rule.
func CheckCredential(username, domain, password string) error {
	u, err := windows.UTF16PtrFromString(username)
	if err != nil {
		return err
	}
	var d *uint16
	if domain != "" {
		d, err = windows.UTF16PtrFromString(domain)
		if err != nil {
			return err
		}
	}
	p, err := windows.UTF16PtrFromString(password)
	if err != nil {
		return err
	}
	var token windows.Handle
	r, _, callErr := procLogonUserW.Call(
		uintptr(unsafe.Pointer(u)),
		uintptr(unsafe.Pointer(d)),
		uintptr(unsafe.Pointer(p)),
		uintptr(logon32LogonInteractive),
		uintptr(logon32ProviderDefault),
		uintptr(unsafe.Pointer(&token)))
	if r == 0 {
		return callErr
	}
	windows.CloseHandle(token)
	return nil
}
