//go:build !windows

package session

func inShadowSession() (bool, error) {
	return false, nil
}
