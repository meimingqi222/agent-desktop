// Package mcp exposes the DesktopService as MCP tools, one per CLI
// command, with a `surface` argument on each.
package mcp

import (
	"context"
	"fmt"

	"github.com/mark3labs/mcp-go/server"

	"github.com/meimingqi222/agent-desktop-shadow/internal/desktop"
)

const (
	serverName    = "agent-desktop-shadow"
	serverVersion = "0.1.0"
)

// Serve runs the MCP server over stdio or SSE until ctx ends.
func Serve(ctx context.Context, svc *desktop.Service, mode, addr string) error {
	s := server.NewMCPServer(serverName, serverVersion,
		server.WithToolCapabilities(true))
	registerTools(s, svc)
	switch mode {
	case "sse":
		if addr == "" {
			addr = "127.0.0.1:8788"
		}
		sse := server.NewSSEServer(s)
		go func() {
			<-ctx.Done()
			sse.Shutdown(ctx)
		}()
		return sse.Start(addr)
	default:
		return server.ServeStdio(s)
	}
}

// registerTools wires every desktop_* tool to the service.
func registerTools(s *server.MCPServer, svc *desktop.Service) {
	addDesktopTools(s, svc)
}

var _ = fmt.Sprintf // keep fmt imported for future tool args
