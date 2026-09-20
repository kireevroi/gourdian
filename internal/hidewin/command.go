package hidewin

import (
	"os"
	"os/exec"
)

// WindowsCommand runs a Windows binary from a Windows directory, since Windows tools
// launched from WSL can't start in a Linux working directory. The window stays hidden,
// so a windowless app doesn't flash a console over the game.
func WindowsCommand(name string, args ...string) *exec.Cmd {
	cmd := exec.Command(name, args...)
	Apply(cmd)
	if _, err := os.Stat("/mnt/c"); err == nil {
		cmd.Dir = "/mnt/c"
	}
	return cmd
}
