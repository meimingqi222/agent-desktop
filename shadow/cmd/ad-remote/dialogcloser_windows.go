//go:build windows

package main

// dialog_auto_closer: polls the shadow session's top-level windows for
// whitelisted system dialogs (crash reports, hard-error boxes) and
// dismisses them. A modal dialog nobody can click would otherwise stall
// every forwarded command. Only known system UI is touched; dialogs
// raised by the agent's own applications are left alone. Set
// AD_DIALOG_CLOSER=0 to disable.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	wmClose    = 0x0010
	bmClick    = 0x00F5
	dlgClass   = "#32770"
	pollEvery  = 750 * time.Millisecond
	skipRecent = 5 * time.Second
)

var (
	user32                     = windows.NewLazySystemDLL("user32.dll")
	procEnumWindows            = user32.NewProc("EnumWindows")
	procEnumChildWindows       = user32.NewProc("EnumChildWindows")
	procGetClassNameW          = user32.NewProc("GetClassNameW")
	procGetWindowTextW         = user32.NewProc("GetWindowTextW")
	procGetWindowTextLengthW   = user32.NewProc("GetWindowTextLengthW")
	procGetWindowThreadProcess = user32.NewProc("GetWindowThreadProcessId")
	procIsWindowVisible        = user32.NewProc("IsWindowVisible")
	procSendMessageW           = user32.NewProc("SendMessageW")
	procPostMessageW           = user32.NewProc("PostMessageW")
	procQueryFullProcessImage  = windows.NewLazySystemDLL("kernel32.dll").NewProc("QueryFullProcessImageNameW")
)

// Owner processes whose dialogs are always safe to dismiss.
var closerProcWhitelist = map[string]bool{
	"werfault.exe":       true,
	"werfaultsecure.exe": true,
	"dwwin.exe":          true,
	"dw20.exe":           true,
}

// System processes allowed to own a dialog closed by title match. A
// user application could reuse a title like "X - System Error", so
// generic titles only close when a system process raised them.
var closerSystemOwners = map[string]bool{
	"csrss.exe":    true,
	"svchost.exe":  true,
	"smss.exe":     true,
	"wininit.exe":  true,
	"winlogon.exe": true,
}

// Dialog titles that identify system UI whatever process owns them
// (the .NET unhandled-exception dialog belongs to the faulting app but
// is itself system error UI). Matched case-insensitively.
var closerTitleAnyOwner = []string{
	"microsoft .net framework",
	"bad image",
	"损坏的图像",
	"windows error reporting",
}

// Dialog titles that only count as system error UI when a system
// process owns the dialog.
var closerTitleSystemOwner = []string{
	"system error",
	"exception processing message",
	"windows - application error",
	"应用程序错误",
	"系统错误",
	"错误报告",
}

// Dismiss-button captions, most preferred first. Never click
// affirmative actions (retry, debug, send) — the agent must see the
// failure surface in its own commands, not have it swallowed.
var closerButtonPrefs = []string{
	"don't send", "do not send", "不发送",
	"cancel", "取消",
	"no", "否",
	"close program", "关闭程序",
	"close", "关闭",
	"ok", "确定",
	"quit", "退出",
	// "Continue" is deliberately absent: on an unhandled-exception
	// dialog it would swallow the fault and keep a broken app running.
}

func startDialogCloser(ctx context.Context) {
	if os.Getenv("AD_DIALOG_CLOSER") == "0" {
		return
	}
	go func() {
		recent := map[uintptr]time.Time{}
		t := time.NewTicker(pollEvery)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				sweepDialogs(recent)
			}
		}
	}()
}

// EnumWindows/EnumChildWindows callbacks are allocated once: each
// syscall.NewCallback consumes a process-wide callback slot that is
// never released, so creating them per sweep would exhaust the table.
// The closer is the only goroutine that runs them, and never
// concurrently with itself.
var (
	enumTopSink []uintptr
	enumTopCB   = syscall.NewCallback(func(hwnd uintptr, _ uintptr) uintptr {
		enumTopSink = append(enumTopSink, hwnd)
		return 1
	})
	enumChildSink []uintptr
	enumChildCB   = syscall.NewCallback(func(hwnd uintptr, _ uintptr) uintptr {
		enumChildSink = append(enumChildSink, hwnd)
		return 1
	})
)

func sweepDialogs(recent map[uintptr]time.Time) {
	enumTopSink = enumTopSink[:0]
	procEnumWindows.Call(enumTopCB, 0)
	tops := enumTopSink

	now := time.Now()
	for hwnd, at := range recent {
		if now.Sub(at) > skipRecent {
			delete(recent, hwnd)
		}
	}
	for _, hwnd := range tops {
		if _, seen := recent[hwnd]; seen || !isDialog(hwnd) {
			continue
		}
		title := windowText(hwnd)
		if !closable(hwnd, title) {
			continue
		}
		fmt.Fprintf(os.Stderr, "dialog-closer: dismissing %q (pid %d)\n", title, windowPid(hwnd))
		recent[hwnd] = now
		if !clickDismiss(hwnd) {
			procPostMessageW.Call(hwnd, wmClose, 0, 0)
		}
	}
}

func isDialog(hwnd uintptr) bool {
	if r, _, _ := procIsWindowVisible.Call(hwnd); r == 0 {
		return false
	}
	buf := make([]uint16, 64)
	n, _, _ := procGetClassNameW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	return windows.UTF16ToString(buf[:n]) == dlgClass
}

func closable(hwnd uintptr, title string) bool {
	owner := windowImageName(hwnd)
	if closerProcWhitelist[owner] {
		return true
	}
	low := strings.ToLower(title)
	for _, want := range closerTitleAnyOwner {
		if strings.Contains(low, want) {
			return true
		}
	}
	if !closerSystemOwners[owner] {
		return false
	}
	for _, want := range closerTitleSystemOwner {
		if strings.Contains(low, want) {
			return true
		}
	}
	return false
}

// clickDismiss presses the best dismiss button on the dialog. A button
// is only pressed when its caption is on the preference list.
func clickDismiss(hwnd uintptr) bool {
	enumChildSink = enumChildSink[:0]
	procEnumChildWindows.Call(hwnd, enumChildCB, 0)
	kids := enumChildSink

	type cand struct {
		hwnd uintptr
		prio int
	}
	var best *cand
	for _, child := range kids {
		buf := make([]uint16, 64)
		n, _, _ := procGetClassNameW.Call(child, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
		if !strings.EqualFold(windows.UTF16ToString(buf[:n]), "button") {
			continue
		}
		text := strings.ToLower(windowText(child))
		for i, want := range closerButtonPrefs {
			if text == want || strings.Contains(text, want) {
				if best == nil || i < best.prio {
					best = &cand{child, i}
				}
				break
			}
		}
	}
	if best == nil {
		return false
	}
	procSendMessageW.Call(best.hwnd, bmClick, 0, 0)
	return true
}

func windowText(hwnd uintptr) string {
	n, _, _ := procGetWindowTextLengthW.Call(hwnd)
	if n == 0 {
		return ""
	}
	buf := make([]uint16, n+1)
	procGetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), n+1)
	return windows.UTF16ToString(buf[:n])
}

func windowPid(hwnd uintptr) uint32 {
	var pid uint32
	procGetWindowThreadProcess.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
	return pid
}

func windowImageName(hwnd uintptr) string {
	pid := windowPid(hwnd)
	if pid == 0 {
		return ""
	}
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return ""
	}
	defer windows.CloseHandle(h)
	buf := make([]uint16, 512)
	size := uint32(len(buf))
	r, _, _ := procQueryFullProcessImage.Call(uintptr(h), 0,
		uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size)))
	if r == 0 {
		return ""
	}
	return strings.ToLower(filepath.Base(windows.UTF16ToString(buf[:size])))
}
