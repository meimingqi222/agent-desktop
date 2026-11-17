package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	gomcp "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/meimingqi222/agent-desktop-shadow/internal/desktop"
	"github.com/meimingqi222/agent-desktop-shadow/internal/surface"
)

var surfaceProp = gomcp.WithString("surface",
	gomcp.Description("target desktop plane"),
	gomcp.Enum("main", "pip"),
	gomcp.DefaultString("pip"))

var authTokenProp = gomcp.WithString("auth_token",
	gomcp.Description("required when surface=main; must match AD_AUTH_TOKEN on the host"))

func addDesktopTools(s *server.MCPServer, svc *desktop.Service) {
	s.AddTool(gomcp.NewTool("desktop_status",
		gomcp.WithDescription("Platform, permission and surface status, including shadow-session capability")),
		func(ctx context.Context, req gomcp.CallToolRequest) (*gomcp.CallToolResult, error) {
			env, err := svc.Status(ctx)
			return resultOf(env, err)
		})

	s.AddTool(gomcp.NewTool("desktop_snapshot",
		gomcp.WithDescription("UIA accessibility tree with ref ids for an app window"),
		surfaceProp, authTokenProp,
		gomcp.WithString("app", gomcp.Description("application name to snapshot, e.g. notepad.exe")),
		gomcp.WithBoolean("interactive", gomcp.Description("interactive elements only (-i)")),
		gomcp.WithNumber("max_depth", gomcp.Description("tree depth cap"))),
		forward(svc, "snapshot", func(req gomcp.CallToolRequest) []string {
			var a []string
			if v := req.GetString("app", ""); v != "" {
				a = append(a, "--app", v)
			}
			if req.GetBool("interactive", false) {
				a = append(a, "-i")
			}
			if d := req.GetFloat("max_depth", 0); d > 0 {
				a = append(a, "--max-depth", fmt.Sprint(int(d)))
			}
			return a
		}))

	s.AddTool(gomcp.NewTool("desktop_click",
		gomcp.WithDescription("Click an element by ref, e.g. @s8f3k2p9:e8"),
		surfaceProp, authTokenProp,
		gomcp.WithString("ref", gomcp.Required(), gomcp.Description("qualified ref"))),
		forward(svc, "click", func(req gomcp.CallToolRequest) []string {
			return []string{req.GetString("ref", "")}
		}))

	s.AddTool(gomcp.NewTool("desktop_type",
		gomcp.WithDescription("Type text into an element by ref"),
		surfaceProp, authTokenProp,
		gomcp.WithString("ref", gomcp.Required(), gomcp.Description("qualified ref")),
		gomcp.WithString("text", gomcp.Required(), gomcp.Description("text to type"))),
		forward(svc, "type", func(req gomcp.CallToolRequest) []string {
			return []string{req.GetString("ref", ""), req.GetString("text", "")}
		}))

	s.AddTool(gomcp.NewTool("desktop_press",
		gomcp.WithDescription("Press a key or combo on the focused element"),
		surfaceProp, authTokenProp,
		gomcp.WithString("keys", gomcp.Required(), gomcp.Description("keys, e.g. \"enter\" or \"ctrl+c\""))),
		forward(svc, "press", func(req gomcp.CallToolRequest) []string {
			return []string{req.GetString("keys", "")}
		}))

	s.AddTool(gomcp.NewTool("desktop_screenshot",
		gomcp.WithDescription("PNG screenshot of a window or display; returns image content"),
		surfaceProp, authTokenProp,
		gomcp.WithString("app", gomcp.Description("application name")),
		gomcp.WithNumber("screen", gomcp.Description("display index"))),
		screenshot(svc))

	s.AddTool(gomcp.NewTool("desktop_launch",
		gomcp.WithDescription("Launch an application on the surface"),
		surfaceProp, authTokenProp,
		gomcp.WithString("app", gomcp.Required(), gomcp.Description("exe path or app name")),
		gomcp.WithBoolean("activate", gomcp.Description("wait for a window")),
		gomcp.WithNumber("timeout", gomcp.Description("ms cap, default 30000"))),
		forward(svc, "launch", func(req gomcp.CallToolRequest) []string {
			a := []string{req.GetString("app", "")}
			if req.GetBool("activate", false) {
				a = append(a, "--activate")
			}
			if t := req.GetFloat("timeout", 0); t > 0 {
				a = append(a, "--timeout", fmt.Sprint(int(t)))
			}
			return a
		}))

	s.AddTool(gomcp.NewTool("desktop_clipboard_get",
		gomcp.WithDescription("Read the surface clipboard"), surfaceProp, authTokenProp),
		forward(svc, "clipboard-get", func(req gomcp.CallToolRequest) []string {
			return nil
		}))

	s.AddTool(gomcp.NewTool("desktop_clipboard_set",
		gomcp.WithDescription("Write the surface clipboard"),
		surfaceProp, authTokenProp,
		gomcp.WithString("text", gomcp.Required())),
		forward(svc, "clipboard-set", func(req gomcp.CallToolRequest) []string {
			return []string{req.GetString("text", "")}
		}))

	s.AddTool(gomcp.NewTool("desktop_wait",
		gomcp.WithDescription("Wait for time, an element ref, text, or a window title"),
		surfaceProp, authTokenProp,
		gomcp.WithString("ref", gomcp.Description("qualified ref to wait for")),
		gomcp.WithString("text", gomcp.Description("text to wait for in the tree")),
		gomcp.WithString("window", gomcp.Description("window title to wait for")),
		gomcp.WithNumber("ms", gomcp.Description("fixed pause in ms"))),
		forward(svc, "wait", func(req gomcp.CallToolRequest) []string {
			var a []string
			if v := req.GetString("ref", ""); v != "" {
				a = append(a, "--element", v)
			}
			if v := req.GetString("text", ""); v != "" {
				a = append(a, "--text", v)
			}
			if v := req.GetString("window", ""); v != "" {
				a = append(a, "--window", v)
			}
			if t := req.GetFloat("ms", 0); t > 0 {
				a = append(a, fmt.Sprint(int(t)))
			}
			return a
		}))

	s.AddTool(gomcp.NewTool("desktop_mouse",
		gomcp.WithDescription("Physical mouse event (move/click/wheel) via the surface input path"),
		surfaceProp, authTokenProp,
		gomcp.WithString("action", gomcp.Required(), gomcp.Enum("move", "click", "wheel")),
		gomcp.WithNumber("x"), gomcp.WithNumber("y"),
		gomcp.WithString("button", gomcp.Enum("left", "right", "middle")),
		gomcp.WithNumber("delta")),
		func(ctx context.Context, req gomcp.CallToolRequest) (*gomcp.CallToolResult, error) {
			action := req.GetString("action", "")
			command := "mouse-" + action
			if !desktop.Forwardable[command] {
				return resultOf(desktop.NewError(command, desktop.CodeInvalidArgs,
					"unknown mouse action", "want move|click|wheel"), nil)
			}
			x := req.GetFloat("x", 0)
			y := req.GetFloat("y", 0)
			var args []string
			if action == "wheel" {
				args = []string{fmt.Sprintf("--x=%s", num(x)), fmt.Sprintf("--y=%s", num(y))}
				if d := req.GetFloat("delta", 0); d != 0 {
					args = append(args, fmt.Sprintf("--dy=%s", num(d)))
				}
			} else {
				// --flag=value form keeps negative coordinates parseable.
				args = []string{fmt.Sprintf("--xy=%s,%s", num(x), num(y))}
				if b := req.GetString("button", ""); b != "" && action == "click" {
					args = append(args, "--button", b)
				}
			}
			args = append(args, "--headed")
			surf, err := targetSurface(svc, req)
			if err != nil {
				return resultOf(desktop.NewError(command, desktop.CodeInvalidArgs, err.Error(), ""), nil)
			}
			env, err := svc.ExecOnForCaller(ctx, surf, req.GetString("auth_token", ""), command, args)
			return resultOf(env, err)
		})

	s.AddTool(gomcp.NewTool("desktop_surface_open",
		gomcp.WithDescription("Create or attach the shadow session. Credentials are optional: child sessions auto-log on as the calling user; supply username+password only to pre-verify them."),
		gomcp.WithString("username"),
		gomcp.WithString("password"),
		gomcp.WithString("domain"),
		gomcp.WithString("display", gomcp.Description("e.g. 1920x1080")),
		gomcp.WithBoolean("viewer")),
		func(ctx context.Context, req gomcp.CallToolRequest) (*gomcp.CallToolResult, error) {
			opts := surface.OpenOpts{
				Username: req.GetString("username", ""),
				Password: req.GetString("password", ""),
				Domain:   req.GetString("domain", ""),
				Viewer:   req.GetBool("viewer", false),
			}
			if d := req.GetString("display", ""); d != "" {
				fmt.Sscanf(d, "%dx%d", &opts.Width, &opts.Height)
			}
			info, err := svc.Manager().Open(ctx, opts)
			if err != nil {
				return stateResult("surface open", err)
			}
			env, err := desktop.NewData("surface open", info)
			return resultOf(env, err)
		})

	s.AddTool(gomcp.NewTool("desktop_surface_switch",
		gomcp.WithDescription("Switch the default surface; main requires an auth token"),
		gomcp.WithString("target", gomcp.Required(), gomcp.Enum("main", "pip")),
		gomcp.WithString("auth_token")),
		func(ctx context.Context, req gomcp.CallToolRequest) (*gomcp.CallToolResult, error) {
			target, err := desktop.ParseSurface(req.GetString("target", ""))
			if err != nil {
				return resultOf(desktop.NewError("surface switch", desktop.CodeInvalidArgs, err.Error(), ""), nil)
			}
			if target == desktop.SurfaceMain {
				// Validate only: a persistent grant would leak to other SSE clients.
				if err := svc.AuthGate().Check(req.GetString("auth_token", "")); err != nil {
					return resultOf(desktop.NewError("surface switch", desktop.CodeAuthRequired,
						err.Error(), "set AD_AUTH_TOKEN on the host"), nil)
				}
			}
			svc.SwitchSurface(target)
			env, err := desktop.NewData("surface switch", map[string]string{"surface": string(target)})
			return resultOf(env, err)
		})

	s.AddTool(gomcp.NewTool("desktop_surface_close",
		gomcp.WithDescription("Destroy the shadow session and release the marker")),
		func(ctx context.Context, req gomcp.CallToolRequest) (*gomcp.CallToolResult, error) {
			err := svc.Manager().Close(ctx)
			env, e := desktop.NewData("surface close", map[string]string{"state": "idle"})
			if e != nil {
				return nil, e
			}
			return resultOf(env, err)
		})

	s.AddTool(gomcp.NewTool("desktop_surface_viewer",
		gomcp.WithDescription("Show or hide the live shadow-desktop window on the user's desktop"),
		gomcp.WithBoolean("on", gomcp.Description("show (default) or hide"), gomcp.DefaultBool(true))),
		func(ctx context.Context, req gomcp.CallToolRequest) (*gomcp.CallToolResult, error) {
			on := req.GetBool("on", true)
			if err := svc.Manager().Viewer(ctx, on); err != nil {
				return stateResult("surface viewer", err)
			}
			state := "hidden"
			if on {
				state = "shown"
			}
			env, err := desktop.NewData("surface viewer", map[string]string{"viewer": state})
			return resultOf(env, err)
		})

	// Escape hatch: any agent-desktop command the named tools don't
	// cover (list-windows, scroll, drag, find, batch, ...) is still one
	// tool call away, and upstream additions work without a rebuild.
	s.AddTool(gomcp.NewTool("desktop_exec",
		gomcp.WithDescription("Run any agent-desktop command on a surface, e.g. list-windows, scroll, drag, batch"),
		surfaceProp, authTokenProp,
		gomcp.WithString("command", gomcp.Required(), gomcp.Description("agent-desktop command name")),
		gomcp.WithArray("args", gomcp.WithStringItems(), gomcp.Description("command arguments"))),
		func(ctx context.Context, req gomcp.CallToolRequest) (*gomcp.CallToolResult, error) {
			command := req.GetString("command", "")
			var args []string
			if raw, ok := req.GetArguments()["args"].([]any); ok {
				for _, v := range raw {
					if s, ok := v.(string); ok {
						args = append(args, s)
					}
				}
			}
			// --stdin would read the MCP transport stream itself and
			// hang waiting for EOF. Only the flag matters: after "--"
			// the same text is positional, not an option.
			for _, a := range args {
				if a == "--" {
					break
				}
				if a == "--stdin" {
					return resultOf(desktop.NewError(command, desktop.CodeInvalidArgs,
						"--stdin is not available over MCP",
						"pass the content as a regular argument instead"), nil)
				}
			}
			surf, err := targetSurface(svc, req)
			if err != nil {
				return resultOf(desktop.NewError(command, desktop.CodeInvalidArgs, err.Error(), ""), nil)
			}
			env, err := svc.ExecOnForCaller(ctx, surf, req.GetString("auth_token", ""), command, args)
			return resultOf(env, err)
		})
}

// forward builds a handler that translates tool args into an argv tail
// and execs it on the requested surface. surface=main needs a matching
// auth_token on every call: the SSE server can serve several clients,
// so a grant must never carry across connections.
func forward(svc *desktop.Service, command string, build func(gomcp.CallToolRequest) []string) server.ToolHandlerFunc {
	return func(ctx context.Context, req gomcp.CallToolRequest) (*gomcp.CallToolResult, error) {
		surf, err := targetSurface(svc, req)
		if err != nil {
			return resultOf(desktop.NewError(command, desktop.CodeInvalidArgs, err.Error(), ""), nil)
		}
		env, err := svc.ExecOnForCaller(ctx, surf, req.GetString("auth_token", ""), command, build(req))
		return resultOf(env, err)
	}
}

func targetSurface(svc *desktop.Service, req gomcp.CallToolRequest) (desktop.Surface, error) {
	if s := req.GetString("surface", ""); s != "" {
		return desktop.ParseSurface(s)
	}
	return svc.Surface(), nil
}

func screenshot(svc *desktop.Service) server.ToolHandlerFunc {
	return func(ctx context.Context, req gomcp.CallToolRequest) (*gomcp.CallToolResult, error) {
		var args []string
		if v := req.GetString("app", ""); v != "" {
			args = append(args, "--app", v)
		}
		if n := req.GetFloat("screen", -1); n >= 0 {
			args = append(args, "--screen", fmt.Sprint(int(n)))
		}
		surf, err := targetSurface(svc, req)
		if err != nil {
			return resultOf(desktop.NewError("screenshot", desktop.CodeInvalidArgs, err.Error(), ""), nil)
		}
		env, err := svc.ExecOnForCaller(ctx, surf, req.GetString("auth_token", ""), "screenshot", args)
		if err != nil || env == nil {
			return resultOf(env, err)
		}
		var data struct {
			Image string `json:"data"`
		}
		if env.OK && json.Unmarshal(env.Data, &data) == nil && data.Image != "" {
			return gomcp.NewToolResultImage(string(env.Data), data.Image, "image/png"), nil
		}
		return resultOf(env, err)
	}
}

// num formats a coordinate without truncating fractions: the upstream
// mouse commands take fractional values (e.g. wheel --dy 0.5).
func num(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}

func resultOf(env *desktop.Envelope, err error) (*gomcp.CallToolResult, error) {
	if err != nil {
		return nil, err
	}
	if env == nil {
		return nil, fmt.Errorf("no envelope")
	}
	out, err := json.Marshal(env)
	if err != nil {
		return nil, err
	}
	res := gomcp.NewToolResultText(string(out))
	if !env.OK {
		res.IsError = true
	}
	return res, nil
}

func stateResult(command string, err error) (*gomcp.CallToolResult, error) {
	if se, ok := err.(*surface.StateError); ok {
		return resultOf(desktop.NewError(command, se.Code, se.Message, se.Suggestion), nil)
	}
	return nil, err
}
