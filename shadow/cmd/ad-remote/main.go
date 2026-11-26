// Command ad-remote is the in-session worker supervisor. The Run-key
// instance is a small supervisor: it starts the pipe-serving worker,
// restarts it after a crash, and is restarted by that worker if the
// supervisor itself dies. Both halves self-gate on the child-session
// shape so the same user's ordinary logons stay untouched.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/meimingqi222/agent-desktop-shadow/internal/desktop"
	"github.com/meimingqi222/agent-desktop-shadow/internal/ipc"
	"github.com/meimingqi222/agent-desktop-shadow/internal/session"
)

func main() {
	var (
		pipeName   string
		hostPID    uint
		binary     string
		childOnly  bool
		workerMode bool
	)
	flag.StringVar(&pipeName, "pipe", "", "named pipe to serve; a bare name like ad-pip-3 is expanded to \\\\.\\pipe\\ad-pip-3")
	flag.UintVar(&hostPID, "host-pid", 0, "host process PID trusted to connect; 0 disables the check")
	flag.StringVar(&binary, "agent-desktop-bin", "", "path to agent-desktop.exe inside this session")
	flag.BoolVar(&childOnly, "child", false, "self-gate: run only inside a shadow (child) session; pipe name defaults to ad-pip-<own session id>")
	flag.BoolVar(&workerMode, "worker", false, "serve the pipe in this process instead of supervising a worker")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if childOnly {
		shadow, err := session.InShadowSession()
		if err != nil || !shadow {
			return
		}
		if pipeName == "" {
			id, err := session.CurrentSessionID()
			if err != nil {
				fmt.Fprintf(os.Stderr, "ad-remote: session id: %v\n", err)
				os.Exit(1)
			}
			pipeName = fmt.Sprintf("ad-pip-%d", id)
		}
	}

	if pipeName == "" {
		fmt.Fprintln(os.Stderr, "ad-remote: --pipe is required")
		os.Exit(2)
	}

	cfg := workerConfig{PipeName: pipeName, Binary: binary, HostPID: uint32(hostPID)}
	if childOnly && !workerMode {
		if err := runSupervisor(ctx, cfg); err != nil {
			fmt.Fprintf(os.Stderr, "ad-remote supervisor: %v\n", err)
			os.Exit(1)
		}
		return
	}

	if childOnly && workerMode {
		watchSupervisor(cfg)
	}

	if err := serveWorker(ctx, pipeName, uint32(hostPID), binary); err != nil {
		fmt.Fprintf(os.Stderr, "ad-remote: %v\n", err)
		os.Exit(1)
	}
}

func serveWorker(ctx context.Context, pipeName string, hostPID uint32, binary string) error {
	adapter, err := desktop.NewMainAdapter(binary)
	if err != nil {
		return err
	}

	server, err := ipc.NewServer(pipeName, hostPID, func(ctx context.Context, req *ipc.Request) ([]byte, error) {
		env, err := adapter.ExecWithStdin(ctx, req.Command, req.Args, req.Stdin)
		if err != nil {
			return nil, err
		}
		return marshal(env)
	})
	if err != nil {
		return err
	}
	defer server.Close()

	startDialogCloser(ctx)

	if err := server.Serve(ctx); err != nil {
		return fmt.Errorf("serve: %w", err)
	}
	return nil
}

func marshal(env *desktop.Envelope) ([]byte, error) {
	return json.Marshal(env)
}
