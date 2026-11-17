// Package authz gates operations on the main desktop, matching the
// design's MAIN_DESKTOP_PERMISSION equivalent: acting on the user's
// real desktop needs an explicit grant.
package authz

import (
	"crypto/subtle"
	"errors"
	"os"
)

// TokenEnvVar names the environment variable holding the grant token.
const TokenEnvVar = "AD_AUTH_TOKEN"

// ErrAuthRequired rejects a main-surface switch without a token.
var ErrAuthRequired = errors.New("operating the main desktop requires an authorization token")

// Gate checks tokens for main-desktop operations.
type Gate struct {
	expected string
}

// NewGate builds a gate. expected is normally the AD_AUTH_TOKEN value;
// empty means no token can ever satisfy the gate.
func NewGate(expected string) *Gate {
	if expected == "" {
		expected = os.Getenv(TokenEnvVar)
	}
	return &Gate{expected: expected}
}

// Check returns ErrAuthRequired unless token matches the configured
// grant. Comparison is constant-time.
func (g *Gate) Check(token string) error {
	if g.expected == "" || token == "" {
		return ErrAuthRequired
	}
	if subtle.ConstantTimeCompare([]byte(token), []byte(g.expected)) != 1 {
		return ErrAuthRequired
	}
	return nil
}
