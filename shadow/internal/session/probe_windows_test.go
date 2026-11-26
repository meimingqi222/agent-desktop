//go:build windows

package session

import "testing"

func TestTermServiceQueryDoesNotRequireElevation(t *testing.T) {
	if _, err := termServiceRunning(); err != nil {
		t.Fatalf("TermService status query should use query-only rights: %v", err)
	}
}
