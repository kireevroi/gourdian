//go:build !windows

package hidewin

import "os/exec"

func Apply(cmd *exec.Cmd) {}

// Visible gives the program its own console window, for interactive logins.
func Visible(cmd *exec.Cmd) {}
