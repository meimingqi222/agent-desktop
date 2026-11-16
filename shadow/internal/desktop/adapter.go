package desktop

import "context"

// Adapter executes one command on a surface and returns the
// agent-desktop envelope it produced.
type Adapter interface {
	Exec(ctx context.Context, command string, args []string) (*Envelope, error)
}
