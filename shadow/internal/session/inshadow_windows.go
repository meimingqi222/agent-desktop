//go:build windows

package session

import "strings"

func inShadowSession() (bool, error) {
	id, err := currentSessionID()
	if err != nil {
		return false, err
	}
	infos, err := ListSessions()
	if err != nil {
		return false, err
	}
	for _, s := range infos {
		if s.ID == id {
			return !strings.EqualFold(s.Name, "Console"), nil
		}
	}
	return false, nil
}
