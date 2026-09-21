package platform

import (
	"runtime"
	"testing"
)

func TestWSLAndLinuxDesktopAreApart(t *testing.T) {
	if runtime.GOOS != "linux" {
		if WSL() || LinuxDesktop() {
			t.Fatal("not Linux, yet WSL or a Linux desktop")
		}
		return
	}
	t.Setenv("WSL_DISTRO_NAME", "Ubuntu")
	if !WSL() || LinuxDesktop() {
		t.Fatal("under WSL")
	}
	t.Setenv("WSL_DISTRO_NAME", "")
	if WSL() || !LinuxDesktop() {
		t.Fatal("on a Linux desktop")
	}
}
