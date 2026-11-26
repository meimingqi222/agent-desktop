// Package remote owns best-effort creation of processes inside an
// already logged-on child session. The supported path is the documented
// WTSQueryUserToken + CreateProcessAsUser pair. Windows normally grants
// that only to privileged callers, so unprivileged hosts treat a
// privilege failure as expected and rely on the in-session supervisor
// that was started at the child session's first logon.
package remote

// LaunchOpts describes one process launch inside a target session.
type LaunchOpts struct {
	SessionID  int
	Executable string
	Args       []string
	WorkingDir string
}
