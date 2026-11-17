//go:build !bundle

package main

// Without -tags bundle the helpers are expected next to ad.exe
// already; nothing to extract.
func extractBundledHelpers() {}
