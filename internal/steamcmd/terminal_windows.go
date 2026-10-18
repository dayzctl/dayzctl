//go:build windows

package steamcmd

import (
	"os"

	"golang.org/x/sys/windows"
)

// stdinIsTerminal reports whether stdin is attached to a console, which is
// required for interactive credential (and Steam Guard code) entry.
func stdinIsTerminal() bool {
	var mode uint32
	return windows.GetConsoleMode(windows.Handle(os.Stdin.Fd()), &mode) == nil
}
