//go:build linux

package steamcmd

import (
	"os"

	"golang.org/x/sys/unix"
)

// stdinIsTerminal reports whether stdin is attached to a terminal, which is
// required for interactive credential (and Steam Guard code) entry.
func stdinIsTerminal() bool {
	_, err := unix.IoctlGetTermios(int(os.Stdin.Fd()), unix.TCGETS)
	return err == nil
}
