//go:build !windows

package desktop

import "os/exec"

func hideConsoleWindow(cmd *exec.Cmd) {}
