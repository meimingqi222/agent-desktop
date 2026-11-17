package desktop

import (
	"context"
	"encoding/json"

	"github.com/meimingqi222/agent-desktop-shadow/internal/surface"
)

// PipAdapter forwards commands into the shadow session through the
// surface manager's IPC channel. Until the session state machine
// reaches Ready it answers with the structured error the manager
// reports, which is what the degradation contract requires.
type PipAdapter struct {
	manager *surface.Manager
}

// NewPipAdapter binds the adapter to a surface manager.
func NewPipAdapter(m *surface.Manager) *PipAdapter {
	return &PipAdapter{manager: m}
}

// Exec sends the command to the in-session worker over named pipe IPC.
func (a *PipAdapter) Exec(ctx context.Context, command string, args []string) (*Envelope, error) {
	if !Forwardable[command] {
		return NewError(command, CodeInvalidArgs,
			"command "+command+" is not forwardable to the shadow session",
			"run it with --surface main"), nil
	}
	if err := a.manager.EnsureReady(ctx); err != nil {
		return notReadyEnvelope(command, err), nil
	}
	raw, err := a.manager.Forward(ctx, command, args)
	if err != nil {
		return NewError(command, CodeInternal, err.Error(), ""), nil
	}
	injected, err := InjectSurface(raw, SurfacePip)
	if err != nil {
		return NewError(command, CodeInternal, err.Error(), ""), nil
	}
	var env Envelope
	if err := json.Unmarshal(injected, &env); err != nil {
		return NewError(command, CodeInternal, err.Error(), ""), nil
	}
	return &env, nil
}

func notReadyEnvelope(command string, err error) *Envelope {
	se, ok := err.(*surface.StateError)
	if !ok {
		return NewError(command, CodeInternal, err.Error(), "")
	}
	return NewError(command, se.Code, se.Message, se.Suggestion)
}
