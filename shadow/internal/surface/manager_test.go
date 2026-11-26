package surface

import "testing"

func TestReattachOpenOptsPreserveAgentDesktopBin(t *testing.T) {
	m := NewManager()
	m.SetAgentDesktopBin(`C:/agent desktop/agent-desktop.exe`)
	got := m.reattachOpenOpts()
	if got.AgentDesktopBin != `C:/agent desktop/agent-desktop.exe` {
		t.Fatalf("reattachOpenOpts().AgentDesktopBin = %q", got.AgentDesktopBin)
	}
}

func TestOpenOptsWithDefaultsOnlyFillMissingAgentDesktopBin(t *testing.T) {
	m := NewManager()
	m.SetAgentDesktopBin(`C:/default/agent-desktop.exe`)
	if got := m.openOptsWithDefaults(OpenOpts{}).AgentDesktopBin; got != `C:/default/agent-desktop.exe` {
		t.Fatalf("default AgentDesktopBin = %q", got)
	}
	if got := m.openOptsWithDefaults(OpenOpts{AgentDesktopBin: `C:/explicit/agent-desktop.exe`}).AgentDesktopBin; got != `C:/explicit/agent-desktop.exe` {
		t.Fatalf("explicit AgentDesktopBin = %q", got)
	}
}
