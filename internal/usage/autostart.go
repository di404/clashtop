package usage

import (
	"os"
	"path/filepath"
)

// optOutPath marks that the user removed the background recorder, so the TUI
// won't re-add it.
func optOutPath(dir string) string { return filepath.Join(dir, "autostart-off") }

// OptedOut reports whether the user removed the recorder with `daemon uninstall`.
func OptedOut(dir string) bool {
	_, err := os.Stat(optOutPath(dir))
	return err == nil
}
