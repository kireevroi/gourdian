//go:build !windows

package config

import "testing"

func TestParseRegValue(t *testing.T) {
	out := "\r\nHKEY_CURRENT_USER\\Software\\Microsoft\\Windows\\CurrentVersion\\Uninstall\\{6F4C2B1E-8D2A-4C5B-9E3F-1A7D2C9B4E51}_is1\r\n" +
		"    InstallLocation    REG_SZ    D:\\Games\\Gourdian\\\r\n\r\n"
	if got := parseRegValue(out, "InstallLocation"); got != "/mnt/d/Games/Gourdian" {
		t.Fatalf("got %q", got)
	}
	if got := parseRegValue(out, "DisplayName"); got != "" {
		t.Fatalf("a missing value gave %q", got)
	}
}
