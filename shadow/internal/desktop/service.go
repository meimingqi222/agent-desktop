package desktop

import (
	"context"
	"encoding/json"

	"github.com/meimingqi222/agent-desktop-shadow/internal/authz"
	"github.com/meimingqi222/agent-desktop-shadow/internal/surface"
)

// Service is the single business object behind both the CLI and the
// MCP server. It routes each command to the adapter for the requested
// surface and returns the envelope contract.
type Service struct {
	main    *MainAdapter
	pip     *PipAdapter
	manager *surface.Manager
	gate    *authz.Gate
	surface Surface
}

// Config wires a Service.
type Config struct {
	BinaryPath     string
	DefaultSurface Surface
}

// NewService builds adapters and the surface manager.
func NewService(cfg Config) (*Service, error) {
	def := cfg.DefaultSurface
	if def == "" {
		def = SurfacePip
	}
	svc := &Service{manager: surface.NewManager(), gate: authz.NewGate(""), surface: def}
	main, err := NewMainAdapter(cfg.BinaryPath)
	if err == nil {
		svc.main = main
	}
	svc.pip = NewPipAdapter(svc.manager)
	return svc, nil
}

// Manager exposes the pip lifecycle for surface commands.
func (s *Service) Manager() *surface.Manager {
	return s.manager
}

// AuthGate is the token gate guarding main-desktop operations.
func (s *Service) AuthGate() *authz.Gate {
	return s.gate
}

// Surface is the plane commands default to.
func (s *Service) Surface() Surface {
	return s.surface
}

// SwitchSurface changes the default plane for later commands.
func (s *Service) SwitchSurface(target Surface) {
	s.surface = target
}

// Exec routes one command to the right surface adapter.
func (s *Service) Exec(ctx context.Context, command string, args []string) (*Envelope, error) {
	return s.ExecOn(ctx, s.surface, command, args)
}

// ExecOn routes one command to an explicit surface.
func (s *Service) ExecOn(ctx context.Context, surf Surface, command string, args []string) (*Envelope, error) {
	a := s.adapterFor(surf)
	if a == nil {
		return NewError(command, CodeInternal, "no adapter for surface "+string(surf), ""), nil
	}
	return a.Exec(ctx, command, args)
}

func (s *Service) adapterFor(surf Surface) Adapter {
	switch surf {
	case SurfaceMain:
		return s.main
	case SurfacePip:
		return s.pip
	default:
		return nil
	}
}

// StatusData is the merged shape `ad status` reports: the upstream
// agent-desktop status plus shadow-surface state.
type StatusData struct {
	AgentDesktop json.RawMessage `json:"agent_desktop,omitempty"`
	Surface      Surface         `json:"surface"`
	Surfaces     map[string]any  `json:"surfaces"`
	Binary       string          `json:"agent_desktop_bin,omitempty"`
}

// Status merges agent-desktop status with surface-manager state.
func (s *Service) Status(ctx context.Context) (*Envelope, error) {
	data := &StatusData{
		Surface: s.Surface(),
		Surfaces: map[string]any{
			"pip": s.manager.Current(),
		},
	}
	if s.main != nil {
		data.Binary = s.main.binaryPath
		env, err := s.main.Exec(ctx, "status", nil)
		if err == nil && env.OK && len(env.Data) > 0 {
			data.AgentDesktop = env.Data
		}
	}
	return NewData("status", data)
}
