package autostart

import (
	"os"
	"strings"
	"testing"
)

func TestAutostartEntryOnLinux(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("WSL_DISTRO_NAME", "")
	if Enabled() {
		t.Fatal("enabled before it was set")
	}
	if err := Set(true); err != nil {
		t.Fatal(err)
	}
	path, _ := entryPath()
	data, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(data), "run -background") || !Enabled() {
		t.Fatalf("entry = %q, %v", data, err)
	}
	if err := Set(false); err != nil || Enabled() {
		t.Fatalf("turning it off: %v", err)
	}
}
