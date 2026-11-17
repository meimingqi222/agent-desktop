//go:build !windows

package session

import "errors"

var errNoWTS = errors.New("WTS child sessions are Windows-only")

func ChildSessionsEnabled() (bool, error)      { return false, errNoWTS }
func EnableChildSessions() error             { return errNoWTS }
func ChildSessionID() (uint32, error)        { return 0, errNoWTS }
func DisconnectSession(sessionID uint32) error { return errNoWTS }
func LogoffSession(sessionID uint32) error   { return errNoWTS }
