package session

import "testing"

func TestIsChildWinStationName(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{"7A78855482A04FA781DC#1", true},
		{"0123456789abcdef#12", true},
		{"Console", false},
		{"RDP-Tcp#0", false},
		{"rdp-tcp#3", false},
		{"WinSta0", false},
		{"Service-0x0-3e4$", false},
		{"ABC#1", false},
		{"7A78855482A04FA781DC#", false},
		{"7A78855482A04FA781DC#x", false},
		{"7A78855482A04FA781DCG#1", false},
	}
	for _, tt := range tests {
		if got := IsChildWinStationName(tt.name); got != tt.want {
			t.Errorf("IsChildWinStationName(%q) = %v, want %v", tt.name, got, tt.want)
		}
	}
}
