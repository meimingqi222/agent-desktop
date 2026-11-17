//go:build windows

package session

import (
	"fmt"

	"golang.org/x/sys/windows/registry"
)

const runKeyPath = `Software\Microsoft\Windows\CurrentVersion\Run`
const runValueName = "AdShadowRemote"

// RegisterWorkerRunKey stores the ad-remote launch command in the
// current user's Run key. The child session auto-logs on as the same
// user, so the key fires inside it on first logon; ad-remote's
// --child gate makes it a no-op in every other session.
func RegisterWorkerRunKey(cmdline string) error {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, runKeyPath, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("open Run key: %w", err)
	}
	defer k.Close()
	return k.SetStringValue(runValueName, cmdline)
}

// UnregisterWorkerRunKey removes the worker launch entry.
func UnregisterWorkerRunKey() error {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	return k.DeleteValue(runValueName)
}

// CurrentSessionID returns the WTS session id of the calling process.
func CurrentSessionID() (int, error) {
	id, err := currentSessionID()
	return int(id), err
}
