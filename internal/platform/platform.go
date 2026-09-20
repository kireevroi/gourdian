// Package platform tells which kind of machine the trainer runs on. A Linux build is either on
// a Linux desktop, where it is the whole app, or under WSL, next to the Windows app that does
// the real work. It reads the environment every time, so tests can pretend to be either.
package platform

import (
	"os"
	"runtime"
)

// WSL reports a Linux build running under WSL.
func WSL() bool { return runtime.GOOS == "linux" && os.Getenv("WSL_DISTRO_NAME") != "" }

// LinuxDesktop reports a Linux build on a Linux desktop, not under WSL.
func LinuxDesktop() bool { return runtime.GOOS == "linux" && os.Getenv("WSL_DISTRO_NAME") == "" }
