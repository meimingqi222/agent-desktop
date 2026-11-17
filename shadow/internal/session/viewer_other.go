//go:build !windows

package session

import "errors"

var errNoViewer = errors.New("shadow viewer control is Windows-only")

// ViewerCommand is unsupported off Windows.
func ViewerCommand(string) error { return errNoViewer }

// ViewerPipeSuffix is unsupported off Windows.
func ViewerPipeSuffix(int) string { return "" }
