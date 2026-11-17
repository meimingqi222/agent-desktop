// Command ad is the single-binary entry to agent-desktop-shadow:
// `ad <command>` forwards to agent-desktop on the selected surface and
// `ad surface|mcp` manages the shadow session and the MCP server.
package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"golang.org/x/term"

	"github.com/meimingqi222/agent-desktop-shadow/internal/desktop"
	"github.com/meimingqi222/agent-desktop-shadow/internal/mcp"
	"github.com/meimingqi222/agent-desktop-shadow/internal/surface"
)

const usage = `ad — agent-desktop-shadow

usage:
  ad <command> [args] [--surface main|pip]   forward a command to agent-desktop
  ad status                                  platform + surface status
  ad surface open [--display WxH] [--viewer] [--username U] [--domain D]
                                  (password via AD_SHADOW_PASSWORD or interactive prompt)
  ad surface switch --target main|pip [--auth-token T]
  ad surface close
  ad surface status
  ad surface viewer [--on|--off]      show/hide the shadow-desktop window
  ad mcp serve [--stdio | --sse addr]

global flags:
  --surface main|pip        target plane (default pip)
  --auth-token T            grant for main-desktop operations (or AD_AUTH_TOKEN)
  --agent-desktop-bin PATH  agent-desktop binary (or AD_AGENT_DESKTOP_BIN)

examples:
  ad status
  ad snapshot --app notepad.exe -i --surface main
  AD_SHADOW_PASSWORD=*** ad surface open --username Administrator
`

type cliConfig struct {
	surface    string
	authToken  string
	binaryPath string
}

func main() {
	code := run(os.Args[1:])
	os.Exit(code)
}

func run(args []string) int {
	cfg, rest, err := parseGlobalFlags(args)
	if err != nil {
		desktop.Write(desktop.NewError("parse", desktop.CodeInvalidArgs, err.Error(), ""))
		return desktop.ExitArgs
	}
	if len(rest) == 0 {
		fmt.Print(usage)
		return desktop.ExitOK
	}

	svc, err := desktop.NewService(desktop.Config{BinaryPath: cfg.binaryPath})
	if err != nil {
		desktop.Write(desktop.NewError("init", desktop.CodeInternal, err.Error(), ""))
		return desktop.ExitError
	}
	if cfg.surface != "" {
		s, err := desktop.ParseSurface(cfg.surface)
		if err != nil {
			desktop.Write(desktop.NewError("parse", desktop.CodeInvalidArgs, err.Error(), ""))
			return desktop.ExitArgs
		}
		if s == desktop.SurfaceMain {
			svc.GrantMain(cfg.authToken)
		}
		svc.SwitchSurface(s)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	command := rest[0]
	cmdArgs := rest[1:]

	switch command {
	case "surface":
		return runSurface(ctx, svc, cfg, cmdArgs)
	case "mcp":
		return runMCP(svc, cmdArgs)
	case "status":
		env, err := svc.Status(ctx)
		if err != nil {
			desktop.Write(desktop.NewError(command, desktop.CodeInternal, err.Error(), ""))
			return desktop.ExitError
		}
		desktop.Write(env)
		return exitOf(env)
	case "help", "--help", "-h":
		fmt.Print(usage)
		return desktop.ExitOK
	default:
		env, err := svc.Exec(ctx, command, cmdArgs)
		if err != nil {
			desktop.Write(desktop.NewError(command, desktop.CodeInternal, err.Error(), ""))
			return desktop.ExitError
		}
		desktop.Write(env)
		return exitOf(env)
	}
}

// parseGlobalFlags extracts ad-level flags from anywhere in argv and
// leaves agent-desktop arguments untouched.
func parseGlobalFlags(args []string) (*cliConfig, []string, error) {
	cfg := &cliConfig{}
	var rest []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		var v string
		var hasInline bool
		if idx := strings.IndexByte(a, '='); idx > 0 {
			v, hasInline = a[idx+1:], true
			a = a[:idx]
		}
		switch a {
		case "--surface":
			if !hasInline {
				i++
				if i >= len(args) {
					return nil, nil, fmt.Errorf("--surface needs a value")
				}
				v = args[i]
			}
			cfg.surface = v
		case "--auth-token":
			if !hasInline {
				i++
				if i >= len(args) {
					return nil, nil, fmt.Errorf("--auth-token needs a value")
				}
				v = args[i]
			}
			cfg.authToken = v
		case "--agent-desktop-bin":
			if !hasInline {
				i++
				if i >= len(args) {
					return nil, nil, fmt.Errorf("--agent-desktop-bin needs a value")
				}
				v = args[i]
			}
			cfg.binaryPath = v
		default:
			if hasInline {
				rest = append(rest, a+"="+v)
			} else {
				rest = append(rest, a)
			}
		}
	}
	return cfg, rest, nil
}

func runSurface(ctx context.Context, svc *desktop.Service, cfg *cliConfig, args []string) int {
	if len(args) == 0 {
		desktop.Write(desktop.NewError("surface", desktop.CodeInvalidArgs,
			"surface needs a subcommand: open|switch|close|status", ""))
		return desktop.ExitArgs
	}
	sub, subArgs := args[0], args[1:]
	switch sub {
	case "open":
		opts := surface.OpenOpts{AgentDesktopBin: cfg.binaryPath}
		for i := 0; i < len(subArgs); i++ {
			switch subArgs[i] {
			case "--display":
				i++
				if i >= len(subArgs) {
					return failArgs("surface open", "--display needs WxH")
				}
				fmt.Sscanf(subArgs[i], "%dx%d", &opts.Width, &opts.Height)
			case "--viewer":
				opts.Viewer = true
			case "--username":
				i++
				if i >= len(subArgs) {
					return failArgs("surface open", "--username needs a value")
				}
				opts.Username = subArgs[i]
			case "--domain":
				i++
				if i >= len(subArgs) {
					return failArgs("surface open", "--domain needs a value")
				}
				opts.Domain = subArgs[i]
			}
		}
		if opts.Username == "" {
			opts.Username = os.Getenv("AD_SHADOW_USERNAME")
		}
		if opts.Password == "" {
			opts.Password = os.Getenv("AD_SHADOW_PASSWORD")
		}
		if opts.Username == "" {
			opts.Username = os.Getenv("USERNAME")
		}
		// A password is optional: WTS child sessions auto-log on as the
		// parent session's user. When supplied it is verified first.
		if opts.Password == "" {
			if pw, err := promptPassword(); err == nil && pw != "" {
				opts.Password = pw
			}
		}
		info, err := svc.Manager().Open(ctx, opts)
		if err != nil {
			writeStateError("surface open", err)
			return desktop.ExitError
		}
		env, err := desktop.NewData("surface open", info)
		if err != nil {
			return failInternal("surface open", err)
		}
		desktop.Write(env)
		return desktop.ExitOK

	case "switch":
		var target string
		for i := 0; i < len(subArgs); i++ {
			if subArgs[i] == "--target" {
				i++
				if i >= len(subArgs) {
					return failArgs("surface switch", "--target needs a value")
				}
				target = subArgs[i]
			}
		}
		s, err := desktop.ParseSurface(target)
		if err != nil {
			desktop.Write(desktop.NewError("surface switch", desktop.CodeInvalidArgs, err.Error(), ""))
			return desktop.ExitArgs
		}
		if s == desktop.SurfaceMain {
			if err := svc.GrantMain(cfg.authToken); err != nil {
				desktop.Write(desktop.NewError("surface switch", desktop.CodeAuthRequired,
					"switching to the main desktop requires --auth-token matching AD_AUTH_TOKEN",
					"set AD_AUTH_TOKEN on the host and pass --auth-token, or use --surface pip"))
				return desktop.ExitError
			}
		}
		svc.SwitchSurface(s)
		env, err := desktop.NewData("surface switch", map[string]string{"surface": string(s)})
		if err != nil {
			return failInternal("surface switch", err)
		}
		desktop.Write(env)
		return desktop.ExitOK

	case "close":
		if err := svc.Manager().Close(ctx); err != nil {
			return failInternal("surface close", err)
		}
		env, err := desktop.NewData("surface close", map[string]string{"state": "idle"})
		if err != nil {
			return failInternal("surface close", err)
		}
		desktop.Write(env)
		return desktop.ExitOK

	case "status":
		env, err := desktop.NewData("surface status", svc.Manager().Current())
		if err != nil {
			return failInternal("surface status", err)
		}
		desktop.Write(env)
		return desktop.ExitOK

	case "viewer":
		show := true
		for _, a := range subArgs {
			switch a {
			case "off", "--off", "hide":
				show = false
			case "on", "--on", "show":
				show = true
			default:
				return failArgs("surface viewer", "unknown flag "+a)
			}
		}
		if err := svc.Manager().Viewer(ctx, show); err != nil {
			writeStateError("surface viewer", err)
			return desktop.ExitError
		}
		state := "hidden"
		if show {
			state = "shown"
		}
		env, err := desktop.NewData("surface viewer", map[string]string{"viewer": state})
		if err != nil {
			return failInternal("surface viewer", err)
		}
		desktop.Write(env)
		return desktop.ExitOK

	default:
		return failArgs("surface", "unknown subcommand "+sub)
	}
}

func runMCP(svc *desktop.Service, args []string) int {
	if len(args) == 0 || args[0] != "serve" {
		return failArgs("mcp", "expected `ad mcp serve [--stdio|--sse addr]`")
	}
	mode := "stdio"
	addr := ""
	for _, a := range args[1:] {
		switch {
		case a == "--stdio":
			mode = "stdio"
		case a == "--sse":
			mode = "sse"
		case strings.HasPrefix(a, "--sse="):
			mode = "sse"
			addr = strings.TrimPrefix(a, "--sse=")
		default:
			addr = a
			mode = "sse"
		}
	}
	if err := mcp.Serve(context.Background(), svc, mode, addr); err != nil {
		fmt.Fprintf(os.Stderr, "ad mcp serve: %v\n", err)
		return desktop.ExitError
	}
	return desktop.ExitOK
}

func writeStateError(command string, err error) {
	if se, ok := err.(*surface.StateError); ok {
		desktop.Write(desktop.NewError(command, se.Code, se.Message, se.Suggestion))
		return
	}
	desktop.Write(desktop.NewError(command, desktop.CodeInternal, err.Error(), ""))
}

func failArgs(command, msg string) int {
	desktop.Write(desktop.NewError(command, desktop.CodeInvalidArgs, msg, ""))
	return desktop.ExitArgs
}

func failInternal(command string, err error) int {
	desktop.Write(desktop.NewError(command, desktop.CodeInternal, err.Error(), ""))
	return desktop.ExitError
}

func exitOf(env *desktop.Envelope) int {
	if env.OK {
		return desktop.ExitOK
	}
	return desktop.ExitError
}

// promptPassword reads the Windows account password interactively so
// credentials never appear on a command line.
func promptPassword() (string, error) {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return "", fmt.Errorf("stdin is not a terminal")
	}
	fmt.Fprint(os.Stderr, "Windows password: ")
	pw, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", err
	}
	return string(pw), nil
}
