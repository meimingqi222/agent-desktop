//go:build windows

package desktop

import (
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

func hideConsoleWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NO_WINDOW}
}
