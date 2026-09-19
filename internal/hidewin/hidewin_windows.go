// Package hidewin starts console programs without a console window, which a windowless app
// would otherwise pop up over the game each time it runs one.
package hidewin

import (
	"os/exec"
	"syscall"
)

const (
	createNoWindow   = 0x08000000
	createNewConsole = 0x00000010
)

func Apply(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
}

// Visible gives the program its own console window, for interactive logins.
func Visible(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createNewConsole}
}
