// Package install writes the Game State Integration config into the Dota 2 install.
package install

import (
	"errors"
	"fmt"
	"gourdian/internal/sys/platform"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

const cfgName = "gamestate_integration_dotatrainer.cfg"

var libraryPath = regexp.MustCompile(`"path"\s+"((?:[^"\\]|\\.)*)"`)

func ParseLibraryFolders(r io.Reader) ([]string, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, m := range libraryPath.FindAllStringSubmatch(string(data), -1) {
		out = append(out, strings.ReplaceAll(m[1], `\\`, `\`))
	}
	return out, nil
}

// WSLPath maps a Windows path like E:\SteamLibrary to /mnt/e/SteamLibrary when running under Linux.
func WSLPath(p string) string {
	if runtime.GOOS == "windows" || len(p) < 3 || p[1] != ':' || (p[2] != '\\' && p[2] != '/') {
		return p
	}
	drive := strings.ToLower(p[:1])
	return "/mnt/" + drive + "/" + strings.ReplaceAll(p[3:], `\`, "/")
}

func steamRoots() []string {
	if runtime.GOOS == "windows" {
		return []string{`C:\Program Files (x86)\Steam`, `C:\Program Files\Steam`}
	}
	home, _ := os.UserHomeDir()
	data := os.Getenv("XDG_DATA_HOME")
	if data == "" {
		data = filepath.Join(home, ".local", "share")
	}
	roots := []string{
		filepath.Join(data, "Steam"),
		filepath.Join(home, ".steam", "steam"),
		filepath.Join(home, ".steam", "root"),
		// Steam installed from Flathub or the Snap store keeps its files in the sandbox.
		filepath.Join(home, ".var", "app", "com.valvesoftware.Steam", ".local", "share", "Steam"),
		filepath.Join(home, ".var", "app", "com.valvesoftware.Steam", "data", "Steam"),
		filepath.Join(home, "snap", "steam", "common", ".local", "share", "Steam"),
	}
	if platform.WSL() {
		roots = append([]string{"/mnt/c/Program Files (x86)/Steam", "/mnt/c/Program Files/Steam"}, roots...)
	}
	return roots
}

// sameDir resolves symlinks, since ~/.steam/steam usually points at ~/.local/share/Steam.
func sameDir(p string) string {
	if real, err := filepath.EvalSymlinks(p); err == nil {
		return real
	}
	return p
}

// FindDota returns every "dota 2 beta" directory found in the known Steam libraries.
func FindDota() []string {
	seen := map[string]bool{}
	var found []string
	for _, root := range steamRoots() {
		libs := []string{root}
		if f, err := os.Open(filepath.Join(root, "steamapps", "libraryfolders.vdf")); err == nil {
			paths, _ := ParseLibraryFolders(f)
			f.Close()
			libs = append(libs, paths...)
		}
		for _, lib := range libs {
			dir := filepath.Join(WSLPath(lib), "steamapps", "common", "dota 2 beta")
			if seen[sameDir(dir)] {
				continue
			}
			seen[sameDir(dir)] = true
			if fi, err := os.Stat(filepath.Join(dir, "game", "dota")); err == nil && fi.IsDir() {
				found = append(found, dir)
			}
		}
	}
	return found
}

func CfgPath(dotaDir string) string {
	return filepath.Join(dotaDir, "game", "dota", "cfg", "gamestate_integration", cfgName)
}

func Render(uri, token string) string {
	return fmt.Sprintf(`"dotatrainer"
{
	"uri"           "%s"
	"timeout"       "5.0"
	"buffer"        "0.1"
	"throttle"      "0.25"
	"heartbeat"     "10.0"
	"auth"
	{
		"token"     "%s"
	}
	"data"
	{
		"provider"  "1"
		"map"       "1"
		"player"    "1"
		"hero"      "1"
		"abilities" "1"
		"items"     "1"
		"events"    "1"
		"buildings" "1"
		"draft"     "1"
	}
}
`, uri, token)
}

func Write(dotaDir, uri, token string) (string, error) {
	path := CfgPath(dotaDir)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	return path, os.WriteFile(path, []byte(Render(uri, token)), 0o644)
}

func Remove(dotaDir string) (string, error) {
	path := CfgPath(dotaDir)
	err := os.Remove(path)
	if errors.Is(err, os.ErrNotExist) {
		return path, nil
	}
	return path, err
}

var (
	appBlock      = regexp.MustCompile(`"570"\s*\{`)
	launchOptions = regexp.MustCompile(`"LaunchOptions"\s+"((?:[^"\\]|\\.)*)"`)
	videoSetting  = regexp.MustCompile(`"setting\.(fullscreen|nowindowborder)"\s+"(\d)"`)
)

func steamRootsWithUserdata() []string {
	var out []string
	seen := map[string]bool{}
	for _, root := range steamRoots() {
		if _, err := os.Stat(filepath.Join(root, "userdata")); err == nil && !seen[sameDir(root)] {
			seen[sameDir(root)] = true
			out = append(out, root)
		}
	}
	return out
}

// LaunchOptions returns Dota's launch options for each Steam account on this machine.
func LaunchOptions() []string {
	var out []string
	for _, root := range steamRootsWithUserdata() {
		files, _ := filepath.Glob(filepath.Join(root, "userdata", "*", "config", "localconfig.vdf"))
		for _, file := range files {
			data, err := os.ReadFile(file)
			if err != nil {
				continue
			}
			if opts, ok := dotaLaunchOptions(data); ok {
				out = append(out, opts)
			}
		}
	}
	return out
}

// dotaLaunchOptions looks only inside "570" blocks, since other games' blocks also carry LaunchOptions.
func dotaLaunchOptions(vdf []byte) (string, bool) {
	for _, loc := range appBlock.FindAllIndex(vdf, -1) {
		block := vdf[loc[1]:]
		if end := bytesIndexClosing(block); end >= 0 {
			block = block[:end]
		}
		if m := launchOptions.FindSubmatch(block); m != nil {
			return strings.ReplaceAll(string(m[1]), `\"`, `"`), true
		}
	}
	return "", false
}

// bytesIndexClosing finds the brace that closes a block whose opening brace was just consumed.
func bytesIndexClosing(b []byte) int {
	depth := 1
	for i, c := range b {
		switch c {
		case '{':
			depth++
		case '}':
			if depth--; depth == 0 {
				return i
			}
		}
	}
	return -1
}

type DisplayMode string

const (
	Exclusive  DisplayMode = "exclusive fullscreen"
	Borderless DisplayMode = "borderless window"
	Windowed   DisplayMode = "windowed"
)

// VideoMode reads Dota's display mode, which decides whether an overlay can be seen.
func VideoMode(dotaDir string) (DisplayMode, error) {
	data, err := os.ReadFile(filepath.Join(dotaDir, "game", "dota", "cfg", "video.txt"))
	if err != nil {
		return "", err
	}
	settings := map[string]string{}
	for _, m := range videoSetting.FindAllStringSubmatch(string(data), -1) {
		settings[m[1]] = m[2]
	}
	switch {
	case settings["fullscreen"] == "1":
		return Exclusive, nil
	case settings["nowindowborder"] == "1":
		return Borderless, nil
	default:
		return Windowed, nil
	}
}
