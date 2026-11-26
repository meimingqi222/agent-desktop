package remote

import "testing"

func TestCommandLineQuoting(t *testing.T) {
	tests := []struct {
		exe  string
		args []string
		want string
	}{
		{`C:\bin\ad-remote.exe`, nil, `C:\bin\ad-remote.exe`},
		{`C:\Program Files\ad\ad-remote.exe`, []string{"--child"}, `"C:\Program Files\ad\ad-remote.exe" --child`},
		{`C:\bin\a.exe`, []string{"a b", `quote"here`, `end\`}, `C:\bin\a.exe "a b" "quote\"here" end\`},
		{`C:\bin\a.exe`, []string{`end with space\`}, `C:\bin\a.exe "end with space\\"`},
		{`C:\bin\a.exe`, []string{""}, `C:\bin\a.exe ""`},
	}
	for _, tt := range tests {
		if got := commandLine(tt.exe, tt.args); got != tt.want {
			t.Errorf("commandLine(%q,%q) = %q, want %q", tt.exe, tt.args, got, tt.want)
		}
	}
}
