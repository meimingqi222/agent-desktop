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
	for _, name := range []string{"agent-desktop.exe", "agent-desktop-win32-x64.exe", "agent-desktop"} {
		path, err := exec.LookPath(name)
		if err != nil {
			continue
		}
		if resolved := resolveToExe(path); resolved != "" {
			return resolved, nil
		}
	}
	return "", fmt.Errorf("agent-desktop binary not found; set %s or install the agent-desktop npm package", BinaryEnvVar)
}

// resolveToExe maps a found path to the real native binary. npm's
// global bin directory only carries the agent-desktop.cmd launcher
// script; the win32 exe sits under the package's own bin dir. Never
// exec a .cmd/.bat through cmd /c: forwarded args would be parsed by
// the shell.
func resolveToExe(path string) string {
	if strings.EqualFold(filepath.Ext(path), ".exe") {
		return path
	}
	dir := filepath.Dir(path)
	for _, candidate := range []string{
		filepath.Join(dir, "agent-desktop-win32-x64.exe"),
		filepath.Join(dir, "agent-desktop-win32-arm64.exe"),
		filepath.Join(dir, "node_modules", "agent-desktop", "bin", "agent-desktop-win32-x64.exe"),
		filepath.Join(dir, "node_modules", "agent-desktop", "bin", "agent-desktop-win32-arm64.exe"),
		// A project-local install puts the shim in <proj>/node_modules/.bin
		// and the native binary in the sibling package's bin dir.
		filepath.Join(dir, "..", "agent-desktop", "bin", "agent-desktop-win32-x64.exe"),
		filepath.Join(dir, "..", "agent-desktop", "bin", "agent-desktop-win32-arm64.exe"),
	} {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate
		}
	}
	return ""
}

// Exec runs `agent-desktop <command> <args...>` and parses the envelope
// from stdout. A non-JSON stdout or a failed launch is folded into an
// INTERNAL envelope so callers always get the contract shape.
func (a *MainAdapter) Exec(ctx context.Context, command string, args []string) (*Envelope, error) {
	return a.ExecWithStdin(ctx, command, args, nil)
}

// ExecWithStdin is Exec with an explicit stdin source. When stdin is
// nil and the args ask for --stdin, the process's own os.Stdin is used.
func (a *MainAdapter) ExecWithStdin(ctx context.Context, command string, args []string, stdin []byte) (*Envelope, error) {
	argv := append([]string{command}, args...)
	cmd := exec.CommandContext(ctx, a.binaryPath, argv...)
	hideConsoleWindow(cmd)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	for _, arg := range args {
		if arg == "--stdin" {
			if stdin != nil {
				cmd.Stdin = bytes.NewReader(stdin)
			} else {
				cmd.Stdin = os.Stdin
			}
			break
		}
	}
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

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
