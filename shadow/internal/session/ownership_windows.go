//go:build windows

package session

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Microsoft/go-winio"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

const (
	ownershipKeyPath      = `Software\AdShadow`
	ownershipSessionValue = "ChildSessionId"
	ownershipStationValue = "ChildWinStation"
)

type childOwnershipRecord struct {
	SessionID      uint32
	WinStationName string
}

// childSessionOwned reports whether the current child session carries
// durable proof that it was created by this implementation. Live pipes
// and the marker can appear in a foreign child through an orphaned Run
// key or a manually started host, so they are diagnostics rather than
// ownership evidence.
func childSessionOwned(childID uint32) bool {
	if childID == 0 || !childSessionAlive(childID) {
		return false
	}
	return ownershipRecordMatches(childID)
}

// OwnedChildSession verifies both halves of ownership before a
// destructive close: the target is this session's WTS child, and it
// matches the durable ownership record written when ad created it.
func OwnedChildSession(sessionID int) bool {
	childID, err := ChildSessionID()
	if err != nil || childID == 0 || int(childID) != sessionID {
		return false
	}
	return childSessionOwned(childID)
}

func probeChildSession() childProbe {
	childID, err := ChildSessionID()
	if err != nil || childID == 0 || !childSessionAlive(childID) {
		return childProbe{ID: -1}
	}
	return childProbe{ID: int(childID), Exists: true, Owned: childSessionOwned(childID)}
}

func recordChildOwnership(sessionID int) error {
	if sessionID <= 0 {
		return fmt.Errorf("invalid child session id %d", sessionID)
	}
	name, err := sessionWinStationName(uint32(sessionID))
	if err != nil {
		return err
	}
	if name == "" {
		return fmt.Errorf("child session %d has no WinStation name", sessionID)
	}
	return writeChildOwnershipRecord(ownershipKeyPath, uint32(sessionID), name)
}

// ClearChildOwnership removes the durable ownership record after the
// matching child session has been logged off.
func ClearChildOwnership() error {
	k, err := registry.OpenKey(registry.CURRENT_USER, ownershipKeyPath, registry.SET_VALUE)
	if err != nil {
		if errors.Is(err, registry.ErrNotExist) {
			return nil
		}
		return err
	}
	defer k.Close()
	var firstErr error
	for _, name := range []string{ownershipSessionValue, ownershipStationValue} {
		if err := k.DeleteValue(name); err != nil && !errors.Is(err, registry.ErrNotExist) && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func ownershipRecordMatches(childID uint32) bool {
	rec, err := readChildOwnershipRecord(ownershipKeyPath)
	if err != nil {
		return false
	}
	currentName, err := sessionWinStationName(childID)
	if err != nil {
		currentName = ""
	}
	return ownershipRecordSatisfied(rec, childID, currentName)
}

func ownershipRecordSatisfied(rec childOwnershipRecord, childID uint32, currentWinStationName string) bool {
	if rec.SessionID != childID {
		return false
	}
	if currentWinStationName == "" {
		return true
	}
	return strings.EqualFold(currentWinStationName, rec.WinStationName)
}

func writeChildOwnershipRecord(keyPath string, sessionID uint32, winStationName string) error {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, keyPath, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("open ownership key: %w", err)
	}
	defer k.Close()
	if err := k.SetStringValue(ownershipStationValue, winStationName); err != nil {
		return fmt.Errorf("write ownership WinStation: %w", err)
	}
	if err := k.SetDWordValue(ownershipSessionValue, sessionID); err != nil {
		return fmt.Errorf("write ownership session id: %w", err)
	}
	return nil
}

func readChildOwnershipRecord(keyPath string) (childOwnershipRecord, error) {
	var rec childOwnershipRecord
	k, err := registry.OpenKey(registry.CURRENT_USER, keyPath, registry.QUERY_VALUE)
	if err != nil {
		return rec, err
	}
	defer k.Close()
	id, _, err := k.GetIntegerValue(ownershipSessionValue)
	if err != nil {
		return rec, err
	}
	name, _, err := k.GetStringValue(ownershipStationValue)
	if err != nil {
		return rec, err
	}
	rec.SessionID = uint32(id)
	rec.WinStationName = name
	return rec, nil
}

// PipePresent reports whether a named pipe exists. A busy pipe still
// counts as present; only ERROR_FILE_NOT_FOUND means absence.
func PipePresent(name string) bool {
	timeout := 30 * time.Millisecond
	conn, err := winio.DialPipe(`\\.\pipe\`+name, &timeout)
	if err == nil {
		conn.Close()
		return true
	}
	return !errors.Is(err, windows.ERROR_FILE_NOT_FOUND)
}
