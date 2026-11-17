package main

import (
	"fmt"
	"golang.org/x/sys/windows"
	"unsafe"
)

func main() {
	wts := windows.NewLazySystemDLL("wtsapi32.dll")
	isEnabled := wts.NewProc("WTSIsChildSessionsEnabled")
	enable := wts.NewProc("WTSEnableChildSessions")
	var en uint32
	r, _, e := isEnabled.Call(uintptr(unsafe.Pointer(&en)))
	fmt.Printf("WTSIsChildSessionsEnabled r=%d enabled=%d err=%v\n", r, en, e)
	r2, _, e2 := enable.Call(1)
	fmt.Printf("WTSEnableChildSessions(TRUE) r=%d err=%v\n", r2, e2)
	r3, _, e3 := isEnabled.Call(uintptr(unsafe.Pointer(&en)))
	fmt.Printf("after: r=%d enabled=%d err=%v\n", r3, en, e3)
}
