//go:build bundle

package main

// Built with `-tags bundle`, ad.exe carries ad-remote.exe and
// ad-rdphost.exe inside itself and extracts them next to the running
// binary on startup, so a single file is a complete install. The
// embedded/ directory is populated by scripts/build.ps1.

import (
	"bytes"
	"embed"
	"fmt"
	"os"
	"path/filepath"
)

//go:embed embedded/ad-remote.exe embedded/ad-rdphost.exe
var embeddedHelpers embed.FS

var bundledExes = []string{"ad-remote.exe", "ad-rdphost.exe"}

func extractBundledHelpers() {
	dir, err := helperDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "ad: cannot resolve install dir: %v\n", err)
		return
	}
	for _, name := range bundledExes {
		data, err := embeddedHelpers.ReadFile("embedded/" + name)
		if err != nil {
			fmt.Fprintf(os.Stderr, "ad: embedded %s unreadable: %v\n", name, err)
			continue
		}
		dst := filepath.Join(dir, name)
		if cur, err := os.ReadFile(dst); err == nil && bytes.Equal(cur, data) {
			continue
		}
		// Write to a temp file then rename: concurrent first runs must
		// never observe a truncated helper. If the rename fails (the
		// existing exe is in use), the old complete file is still valid.
		tmp := dst + ".tmp"
		if err := os.WriteFile(tmp, data, 0o755); err != nil {
			fmt.Fprintf(os.Stderr, "ad: cannot write %s: %v\n", tmp, err)
			continue
		}
		if err := os.Rename(tmp, dst); err != nil {
			os.Remove(tmp)
		}
	}
}

func helperDir() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.Dir(exe), nil
}
