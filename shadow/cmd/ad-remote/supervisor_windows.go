//go:build windows

package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"time"

	"golang.org/x/sys/windows"

	"github.com/meimingqi222/agent-desktop-shadow/internal/session"
)

func runSupervisor(ctx context.Context, cfg workerConfig) error {
	mutex, owned, err := acquireSupervisorMutex()
	if err != nil {
		return err
	}
	if !owned {
		return nil
	}
	defer windows.CloseHandle(mutex)

	backoff := 250 * time.Millisecond
	for {
		started := time.Now()
		cmd, err := startRemoteProcess(cfg, true)
		if err != nil {
			logf("worker start failed: %v", err)
		} else {
			done := make(chan error, 1)
			go func() { done <- cmd.Wait() }()
			select {
			case <-ctx.Done():
				if cmd.Process != nil {
					cmd.Process.Kill()
				}
				<-done
				return nil
			case err := <-done:
				if err != nil {
					logf("worker exited: %v", err)
				} else {
					logf("worker exited")
				}
			}
		}

		if time.Since(started) > 10*time.Second {
			backoff = 250 * time.Millisecond
		} else if backoff < 5*time.Second {
			backoff *= 2
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(backoff):
		}
	}
}

func acquireSupervisorMutex() (windows.Handle, bool, error) {
	id, err := session.CurrentSessionID()
	if err != nil {
		return 0, false, fmt.Errorf("session id: %w", err)
	}
	name, err := windows.UTF16PtrFromString(fmt.Sprintf(`Local\ad-remote-supervisor-%d`, id))
	if err != nil {
		return 0, false, err
	}
	handle, err := windows.CreateMutex(nil, true, name)
	if errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
		if handle != 0 {
			windows.CloseHandle(handle)
		}
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("create supervisor mutex: %w", err)
	}
	return handle, true, nil
}

func startRemoteProcess(cfg workerConfig, worker bool) (*exec.Cmd, error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(exe, cfg.args(worker)...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NO_WINDOW}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return cmd, nil
}

func watchSupervisor(cfg workerConfig) {
	parent := os.Getppid()
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for range ticker.C {
			if !processGone(parent) {
				continue
			}
			if cmd, err := startRemoteProcess(cfg, false); err == nil && cmd.Process != nil {
				cmd.Process.Release()
			}
			os.Exit(0)
		}
	}()
}

func processGone(pid int) bool {
	if pid <= 0 {
		return true
	}
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err == nil {
		windows.CloseHandle(handle)
		return false
	}
	return errors.Is(err, windows.ERROR_INVALID_PARAMETER)
}

func logf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "ad-remote supervisor: "+format+"\n", args...)
}
