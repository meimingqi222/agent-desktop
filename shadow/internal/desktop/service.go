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
	main        *MainAdapter
	pip         *PipAdapter
	manager     *surface.Manager
	gate        *authz.Gate
	surface     Surface
	mainGranted bool
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

// GrantMain validates a token against the gate and, on success, allows
// commands to run on the main desktop for the rest of this process.
func (s *Service) GrantMain(token string) error {
	if err := s.gate.Check(token); err != nil {
		return err
	}
	s.mainGranted = true
	return nil
}

// MainGranted reports whether this process may act on the main desktop.
func (s *Service) MainGranted() bool {
	return s.mainGranted
}

// Exec routes one command to the right surface adapter.
func (s *Service) Exec(ctx context.Context, command string, args []string) (*Envelope, error) {
	return s.ExecOn(ctx, s.surface, command, args)
}

// ExecOn routes one command to an explicit surface. Main-surface
// commands need an earlier GrantMain: the plane that can disturb the
// user's real desktop is never reachable without the token, whichever
// path (flag, switch, MCP arg) selected it.
func (s *Service) ExecOn(ctx context.Context, surf Surface, command string, args []string) (*Envelope, error) {
	if surf == SurfaceMain && !s.mainGranted {
		return NewError(command, CodeAuthRequired,
			"operating the main desktop requires authorization",
			"pass --auth-token matching AD_AUTH_TOKEN, or use --surface pip"), nil
	}
	return s.execOn(ctx, surf, command, args)
}

// ExecOnForCaller runs a command for an unprivileged caller such as an
// MCP connection. Unlike ExecOn it checks the supplied token on every
// call and never turns a successful check into a process-wide grant:
// on a multi-client SSE server, one client's token must not unlock the
// main desktop for every other connection.
func (s *Service) ExecOnForCaller(ctx context.Context, surf Surface, token, command string, args []string) (*Envelope, error) {
	if surf == SurfaceMain {
		if err := s.gate.Check(token); err != nil {
			return NewError(command, CodeAuthRequired,
				"operating the main desktop requires authorization",
				"pass auth_token matching AD_AUTH_TOKEN, or use surface=pip"), nil
		}
	}
	return s.execOn(ctx, surf, command, args)
}

func (s *Service) execOn(ctx context.Context, surf Surface, command string, args []string) (*Envelope, error) {
	a := s.adapterFor(surf)
	if a == nil {
		if surf == SurfaceMain {
			return NewError(command, CodeInternal,
				"agent-desktop binary not found; set "+BinaryEnvVar, ""), nil
		}
		return NewError(command, CodeInternal, "no adapter for surface "+string(surf), ""), nil
	}
	return a.Exec(ctx, command, args)
}

func (s *Service) adapterFor(surf Surface) Adapter {
	switch surf {
	case SurfaceMain:
		if s.main == nil {
			return nil
		}
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
	data.Surfaces["main"] = map[string]any{"authorized": s.mainGranted}
	return NewData("status", data)
}
