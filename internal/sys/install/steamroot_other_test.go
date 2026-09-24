//go:build !windows

package install

import "testing"

func TestRegValueReadsRegQuery(t *testing.T) {
	out := "\r\nHKEY_CURRENT_USER\\Software\\Valve\\Steam\r\n    SteamPath    REG_SZ    d:/games/steam\r\n\r\n"
	if got := regValue(out, "SteamPath"); got != "d:/games/steam" {
		t.Fatalf("SteamPath = %q", got)
	}
	if got := WSLPath("d:/games/steam"); got != "/mnt/d/games/steam" {
		t.Fatalf("WSL path = %q", got)
	}
}
