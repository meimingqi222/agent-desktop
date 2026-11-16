// Command ad-remote is the in-session worker: it runs inside the
// shadow session, listens on a named pipe, and executes each forwarded
// command through the local agent-desktop binary. The host injects it
// with --pipe and --host-pid.
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
)

func main() {
	var (
		pipeName string
		hostPID  uint
		binary   string
	)
	flag.StringVar(&pipeName, "pipe", "", "named pipe to serve; a bare name like ad-pip-3 is expanded to \\\\.\\pipe\\ad-pip-3")
	flag.UintVar(&hostPID, "host-pid", 0, "host process PID trusted to connect; 0 disables the check")
	flag.StringVar(&binary, "agent-desktop-bin", "", "path to agent-desktop.exe inside this session")
	flag.Parse()

	if pipeName == "" {
		fmt.Fprintln(os.Stderr, "ad-remote: --pipe is required")
		os.Exit(2)
	}

	adapter, err := desktop.NewMainAdapter(binary)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ad-remote: %v\n", err)
		os.Exit(1)
	}

	server, err := ipc.NewServer(pipeName, uint32(hostPID), func(ctx context.Context, command string, args []string) ([]byte, error) {
		env, err := adapter.Exec(ctx, command, args)
		if err != nil {
			return nil, err
		}
		return marshal(env)
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "ad-remote: %v\n", err)
		os.Exit(1)
	}
	defer server.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := server.Serve(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "ad-remote: serve: %v\n", err)
		os.Exit(1)
	}
}

func marshal(env *desktop.Envelope) ([]byte, error) {
	return json.Marshal(env)
}
