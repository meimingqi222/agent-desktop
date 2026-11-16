package desktop

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// BinaryEnvVar overrides agent-desktop binary discovery.
const BinaryEnvVar = "AD_AGENT_DESKTOP_BIN"

// MainAdapter forwards commands to agent-desktop.exe running in the
// caller's own session. It is a thin exec wrapper: every argv passes
// through unchanged and the JSON envelope comes back with
// data.surface stamped on it.
type MainAdapter struct {
	binaryPath string
}

// NewMainAdapter resolves the agent-desktop binary.
func NewMainAdapter(binaryPath string) (*MainAdapter, error) {
	path, err := resolveAgentDesktop(binaryPath)
	if err != nil {
		return nil, err
	}
	return &MainAdapter{binaryPath: path}, nil
}

func resolveAgentDesktop(explicit string) (string, error) {
	candidates := []string{explicit, os.Getenv(BinaryEnvVar)}
	if self, err := os.Executable(); err == nil {
		dir := filepath.Dir(self)
		candidates = append(candidates,
			filepath.Join(dir, "agent-desktop.exe"),
			filepath.Join(dir, "agent-desktop-win32-x64.exe"),
			filepath.Join(dir, "agent-desktop"),
		)
	}
	for _, c := range candidates {
		if c == "" {
			continue
		}
		if info, err := os.Stat(c); err == nil && !info.IsDir() {
			return c, nil
		}
	}
	for _, name := range []string{"agent-desktop.exe", "agent-desktop-win32-x64.exe", "agent-desktop.cmd", "agent-desktop"} {
		if path, err := exec.LookPath(name); err == nil {
			return path, nil
		}
	}
	return "", fmt.Errorf("agent-desktop binary not found; set %s or install the agent-desktop npm package", BinaryEnvVar)
}

// Exec runs `agent-desktop <command> <args...>` and parses the envelope
// from stdout. A non-JSON stdout or a failed launch is folded into an
// INTERNAL envelope so callers always get the contract shape.
func (a *MainAdapter) Exec(ctx context.Context, command string, args []string) (*Envelope, error) {
	argv := append([]string{command}, args...)
	cmd := commandFor(ctx, a.binaryPath, argv)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	runErr := cmd.Run()
	out := bytes.TrimSpace(stdout.Bytes())
	if len(out) == 0 {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" && runErr != nil {
			msg = runErr.Error()
		}
		if msg == "" {
			msg = "agent-desktop produced no output"
		}
		return NewError(command, CodeInternal, msg, ""), nil
	}
	if !json.Valid(out) {
		return NewError(command, CodeInternal,
			fmt.Sprintf("agent-desktop emitted non-JSON output: %s", truncate(string(out), 400)), ""), nil
	}
	injected, err := InjectSurface(out, SurfaceMain)
	if err != nil {
		return NewError(command, CodeInternal, err.Error(), ""), nil
	}
	var env Envelope
	if err := json.Unmarshal(injected, &env); err != nil {
		return NewError(command, CodeInternal, err.Error(), ""), nil
	}
	return &env, nil
}

func commandFor(ctx context.Context, binary string, argv []string) *exec.Cmd {
	lower := strings.ToLower(binary)
	if strings.HasSuffix(lower, ".cmd") || strings.HasSuffix(lower, ".bat") {
		return exec.CommandContext(ctx, "cmd", append([]string{"/c", binary}, argv...)...)
	}
	return exec.CommandContext(ctx, binary, argv...)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
