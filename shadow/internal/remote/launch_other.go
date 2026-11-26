//go:build !windows

package remote

import "errors"

// LaunchInSession is Windows-only because WTS sessions are Windows-only.
func LaunchInSession(LaunchOpts) (int, error) {
	return 0, errors.New("cross-session process launch is Windows-only")
}
