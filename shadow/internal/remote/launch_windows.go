//go:build windows

package remote

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

// LaunchInSession starts executable inside an already logged-on WTS
// session using the session user's token. It does not inject into or
// open another process; Windows itself creates the process.
func LaunchInSession(opts LaunchOpts) (int, error) {
	if opts.SessionID <= 0 {
		return 0, fmt.Errorf("invalid target session %d", opts.SessionID)
	}
	if opts.Executable == "" {
		return 0, fmt.Errorf("executable is required")
	}
	var userToken windows.Token
	if err := windows.WTSQueryUserToken(uint32(opts.SessionID), &userToken); err != nil {
		return 0, fmt.Errorf("query user token for session %d: %w", opts.SessionID, err)
	}
	defer userToken.Close()
	var primary windows.Token
	if err := windows.DuplicateTokenEx(userToken,
		windows.TOKEN_QUERY|windows.TOKEN_DUPLICATE|windows.TOKEN_ASSIGN_PRIMARY, nil,
		windows.SecurityImpersonation, windows.TokenPrimary, &primary); err != nil {
		return 0, fmt.Errorf("duplicate session token: %w", err)
	}
	defer primary.Close()

	exe, err := windows.UTF16PtrFromString(opts.Executable)
	if err != nil {
		return 0, err
	}
	cmdline, err := windows.UTF16PtrFromString(commandLine(opts.Executable, opts.Args))
	if err != nil {
		return 0, err
	}
	desktop, err := windows.UTF16PtrFromString(`winsta0\default`)
	if err != nil {
		return 0, err
	}
	var startup windows.StartupInfo
	startup.Cb = uint32(unsafe.Sizeof(startup))
	startup.Desktop = desktop
	var wd *uint16
	if opts.WorkingDir != "" {
		wd, err = windows.UTF16PtrFromString(opts.WorkingDir)
		if err != nil {
			return 0, err
		}
	}
	var pi windows.ProcessInformation
	flags := uint32(windows.CREATE_UNICODE_ENVIRONMENT | windows.CREATE_NO_WINDOW)
	if err := windows.CreateProcessAsUser(primary, exe, cmdline, nil, nil, false,
		flags, nil, wd, &startup, &pi); err != nil {
		return 0, fmt.Errorf("create process in session %d: %w", opts.SessionID, err)
	}
	windows.CloseHandle(pi.Thread)
	windows.CloseHandle(pi.Process)
	return int(pi.ProcessId), nil
}
