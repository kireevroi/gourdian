package autostart

import (
	"errors"
	"gourdian/internal/sys/platform"
	"os"
	"path/filepath"
	"strings"
)

// Linux desktops start what is listed in ~/.config/autostart (the XDG autostart spec, which
// KDE, GNOME, Xfce and the other desktops Arch users run all follow).
const desktopName = "gourdian.desktop"

// legacyDesktopName is the entry the app wrote when it was called Gourdian.
const legacyDesktopName = "dotatrainer.desktop"

func entryPath() (string, error) { return autostartPath(desktopName) }

func autostartPath(name string) (string, error) {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "autostart", name), nil
}

// Exe is the trainer to start at login; under WSL there is none, since Windows runs it.
func Exe() string {
	if platform.WSL() {
		return ""
	}
	self, err := os.Executable()
	if err != nil {
		return ""
	}
	return self
}

func Enabled() bool {
	for _, name := range []string{desktopName, legacyDesktopName} {
		if path, err := autostartPath(name); err == nil {
			if _, err := os.Stat(path); err == nil {
				return true
			}
		}
	}
	return false
}

func Set(enable bool) error {
	path, err := entryPath()
	if err != nil {
		return err
	}
	// The entry written under the old name goes either way; it starts a binary that's gone.
	if legacy, err := autostartPath(legacyDesktopName); err == nil {
		if err := os.Remove(legacy); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	if !enable {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	exe := Exe()
	if exe == "" {
		return errors.New("under WSL the Windows app starts itself; use its setting")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(Entry(exe, "run -background")), 0o644)
}

// Entry is a .desktop file that runs exe with args; the installer writes the same shape
// for the application menu.
func Entry(exe, args string) string {
	quoted := exe
	if strings.ContainsAny(exe, " \t") {
		quoted = `"` + exe + `"`
	}
	return strings.Join([]string{
		"[Desktop Entry]",
		"Type=Application",
		"Name=Gourdian",
		"Comment=Live coaching for Dota 2",
		"Exec=" + quoted + " " + args,
		"Icon=gourdian",
		"Terminal=false",
		"Categories=Game;",
		"X-GNOME-Autostart-enabled=true",
		"",
	}, "\n")
}
