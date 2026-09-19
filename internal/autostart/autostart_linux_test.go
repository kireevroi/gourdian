package autostart

import (
	"os"
	"path/filepath"
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

// Only paths with a space or a tab need quotes in Exec=; the check used to look for a
// backslash or the letter t, quoting /opt/tools/… for nothing.
func TestEntryQuotesOnlyPathsWithBlanks(t *testing.T) {
	for exe, want := range map[string]string{
		"/opt/tools/gourdian":        "Exec=/opt/tools/gourdian run",
		"/home/me/My Games/gourdian": `Exec="/home/me/My Games/gourdian" run`,
		"/home/me/tab\tdir/gourdian": "Exec=\"/home/me/tab\tdir/gourdian\" run",
	} {
		if entry := Entry(exe, "run"); !strings.Contains(entry, want+"\n") {
			t.Errorf("%q: entry has no %q:\n%s", exe, want, entry)
		}
	}
}

// Turning autostart off also removes the entry written under the app's old name.
func TestSetRemovesTheOldNamesEntry(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("WSL_DISTRO_NAME", "")
	legacy, _ := autostartPath(legacyDesktopName)
	if err := os.MkdirAll(filepath.Dir(legacy), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacy, []byte("[Desktop Entry]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !Enabled() {
		t.Fatal("the old name's entry should count as enabled")
	}
	if err := Set(false); err != nil || Enabled() {
		t.Fatalf("still enabled after turning it off: %v", err)
	}
}
