// Package desktop holds the DesktopService that routes commands to a
// surface adapter and re-emits agent-desktop JSON envelopes.
package desktop

import (
	"encoding/json"
	"fmt"
	"runtime"
)

const (
	// EnvelopeVersion matches the agent-desktop output contract.
	EnvelopeVersion = "2.6"

	ExitOK    = 0
	ExitError = 1
	ExitArgs  = 2
)

// Envelope mirrors the agent-desktop JSON output contract.
type Envelope struct {
	Version string          `json:"version"`
	OK      bool            `json:"ok"`
	Command string          `json:"command"`
	Data    json.RawMessage `json:"data,omitempty"`
	Error   *EnvelopeError  `json:"error,omitempty"`
}

// EnvelopeError carries a structured failure back to the caller.
type EnvelopeError struct {
	Code        string          `json:"code"`
	Message     string          `json:"message"`
	Suggestion  string          `json:"suggestion,omitempty"`
	Details     json.RawMessage `json:"details,omitempty"`
	Disposition *Disposition    `json:"disposition,omitempty"`
}

// Disposition reports whether an action was delivered and whether a
// retry is safe.
type Disposition struct {
	Delivery string `json:"delivery"`
	Retry    string `json:"retry"`
}

// Surface names a desktop plane the agent operates on.
type Surface string

const (
	SurfaceMain Surface = "main"
	SurfacePip  Surface = "pip"
)

// ParseSurface validates a surface flag value.
func ParseSurface(s string) (Surface, error) {
	switch Surface(s) {
	case SurfaceMain, SurfacePip:
		return Surface(s), nil
	default:
		return "", fmt.Errorf("invalid surface %q (want main|pip)", s)
	}
}

// Error codes that extend the agent-desktop set for shadow surfaces.
const (
	CodePlatformNotSupported   = "PLATFORM_NOT_SUPPORTED"
	CodeTermServiceUnavailable = "TERM_SERVICE_UNAVAILABLE"
	CodeSurfaceNotReady        = "SURFACE_NOT_READY"
	CodeAuthRequired           = "AUTH_REQUIRED"
	CodeCredentialRejected     = "CREDENTIAL_REJECTED"
	CodeInternal               = "INTERNAL"
	CodeInvalidArgs            = "INVALID_ARGS"
)

// NewError builds a failed envelope for command.
func NewError(command, code, message, suggestion string) *Envelope {
	return &Envelope{
		Version: EnvelopeVersion,
		OK:      false,
		Command: command,
		Error: &EnvelopeError{
			Code:       code,
			Message:    message,
			Suggestion: suggestion,
		},
	}
}

// NewData builds a success envelope carrying data.
func NewData(command string, data any) (*Envelope, error) {
	raw, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}
	return &Envelope{Version: EnvelopeVersion, OK: true, Command: command, Data: raw}, nil
}

// InjectSurface parses an upstream envelope and stamps data.surface so
// callers can tell which plane produced the result.
func InjectSurface(raw []byte, s Surface) ([]byte, error) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		return nil, err
	}
	dataRaw, ok := top["data"]
	if !ok || len(dataRaw) == 0 || string(dataRaw) == "null" {
		return raw, nil
	}
	var data map[string]json.RawMessage
	if err := json.Unmarshal(dataRaw, &data); err != nil {
		return raw, nil
	}
	surfaceRaw, err := json.Marshal(s)
	if err != nil {
		return nil, err
	}
	data["surface"] = surfaceRaw
	merged, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}
	top["data"] = merged
	return json.Marshal(top)
}

// Write prints the envelope as one compact JSON line.
func Write(e *Envelope) {
	out, err := json.Marshal(e)
	if err != nil {
		fmt.Printf(`{"version":%q,"ok":false,"command":%q,"error":{"code":%q,"message":%q}}`,
			EnvelopeVersion, e.Command, CodeInternal, err.Error())
		fmt.Println()
		return
	}
	fmt.Println(string(out))
}

// WriteRaw prints bytes already shaped as an envelope.
func WriteRaw(raw []byte) {
	fmt.Println(string(raw))
}

// PlatformError describes running on a non-Windows host.
func PlatformError(command string) *Envelope {
	return NewError(command, CodePlatformNotSupported,
		fmt.Sprintf("shadow surfaces require Windows 10 1809+; host is %s/%s", runtime.GOOS, runtime.GOARCH),
		"run ad on a Windows host; main-surface commands may still work if agent-desktop supports this platform")
}
