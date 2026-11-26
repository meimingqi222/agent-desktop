package main

import (
	"reflect"
	"testing"
)

func TestWorkerArgs(t *testing.T) {
	cfg := workerConfig{PipeName: "ad-pip-9", Binary: `C:\agent desktop\agent-desktop.exe`, HostPID: 42}
	want := []string{
		"--child", "--pipe", "ad-pip-9",
		"--agent-desktop-bin", `C:\agent desktop\agent-desktop.exe`,
		"--host-pid", "42", "--worker",
	}
	if got := cfg.args(true); !reflect.DeepEqual(got, want) {
		t.Fatalf("args(true) = %#v, want %#v", got, want)
	}
	got := cfg.args(false)
	for _, arg := range got {
		if arg == "--worker" {
			t.Fatalf("supervisor args must not include --worker: %#v", got)
		}
	}
}
