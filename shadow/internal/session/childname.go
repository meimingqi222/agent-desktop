package session

import "strings"

// IsChildWinStationName reports the session-name shape Windows assigns
// to WTS child sessions: a long hexadecimal token followed by #<n>.
// Ordinary interactive stations use stable names such as Console and
// RDP-Tcp#0, so those never match this shape.
func IsChildWinStationName(name string) bool {
	base, suffix, ok := strings.Cut(name, "#")
	if !ok || len(base) < 16 || len(base) > 32 || suffix == "" {
		return false
	}
	for _, r := range base {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') && (r < 'A' || r > 'F') {
			return false
		}
	}
	for _, r := range suffix {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
