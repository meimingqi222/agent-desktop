//go:build !windows

package session

import "errors"

// CheckCredential always fails off Windows; there is no LogonUser.
func CheckCredential(username, domain, password string) error {
	return errors.New("credential validation requires Windows")
}
