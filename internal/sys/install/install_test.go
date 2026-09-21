package install

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

const vdf = `"libraryfolders"
{
	"0"
	{
		"path"		"C:\\Program Files (x86)\\Steam"
		"apps" { "228980" "1" }
	}
	"1"
	{
		"path"		"E:\\SteamLibrary"
		"apps" { "570" "77247992804" }
	}
}`

func TestParseLibraryFolders(t *testing.T) {
	got, err := ParseLibraryFolders(strings.NewReader(vdf))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{`C:\Program Files (x86)\Steam`, `E:\SteamLibrary`}
	if !slices.Equal(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestWSLPath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("WSL mapping only applies off Windows")
	}
	if got := WSLPath(`E:\SteamLibrary\steamapps`); got != "/mnt/e/SteamLibrary/steamapps" {
		t.Fatalf("got %q", got)
	}
	if got := WSLPath("/home/me/steam"); got != "/home/me/steam" {
		t.Fatalf("linux path changed: %q", got)
	}
}

func TestWriteAndRemove(t *testing.T) {
	dir := t.TempDir()
	path, err := Write(dir, "http://127.0.0.1:4570/gsi", "secret")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(dir, "game", "dota", "cfg", "gamestate_integration", cfgName); path != want {
		t.Fatalf("path = %s, want %s", path, want)
	}
	data, _ := os.ReadFile(path)
	for _, s := range []string{`"uri"           "http://127.0.0.1:4570/gsi"`, `"token"     "secret"`, `"events"    "1"`} {
		if !strings.Contains(string(data), s) {
			t.Errorf("config missing %q:\n%s", s, data)
		}
	}
	if _, err := Remove(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("config still present after Remove")
	}
	if _, err := Remove(dir); err != nil {
		t.Fatalf("second Remove should be a no-op: %v", err)
	}
}

func TestVideoMode(t *testing.T) {
	cases := map[string]DisplayMode{
		`"setting.fullscreen"		"1"` + "\n" + `"setting.nowindowborder"		"0"`: Exclusive,
		`"setting.fullscreen"		"0"` + "\n" + `"setting.nowindowborder"		"1"`: Borderless,
		`"setting.fullscreen"		"0"` + "\n" + `"setting.nowindowborder"		"0"`: Windowed,
	}
	for body, want := range cases {
		dir := t.TempDir()
		cfg := filepath.Join(dir, "game", "dota", "cfg")
		os.MkdirAll(cfg, 0o755)
		os.WriteFile(filepath.Join(cfg, "video.txt"), []byte("\"config\"\n{\n"+body+"\n}\n"), 0o644)
		if got, err := VideoMode(dir); err != nil || got != want {
			t.Errorf("VideoMode(%q) = %q, %v; want %q", body, got, err, want)
		}
	}
}

func TestDotaLaunchOptions(t *testing.T) {
	vdf := []byte(`"UserLocalConfigStore"
{
	"Software" { "Valve" { "Steam" { "apps" {
		"440" { "LaunchOptions" "-novid" }
		"570"
		{
			"LastPlayed" "1789000000"
			"cloud" { "last_sync_state" "synchronized" }
			"LaunchOptions" "-gamestateintegration -console"
		}
		"730" { "LaunchOptions" "-high" }
	} } } }
}`)
	got, ok := dotaLaunchOptions(vdf)
	if !ok || got != "-gamestateintegration -console" {
		t.Fatalf("got %q, %v", got, ok)
	}
	if _, ok := dotaLaunchOptions([]byte(`"570" { "LastPlayed" "1" } "730" { "LaunchOptions" "-high" }`)); ok {
		t.Fatal("must not pick up another game's launch options")
	}
}

func TestFindDotaOnLinuxSteam(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Linux Steam layout")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("WSL_DISTRO_NAME", "")
	steam := filepath.Join(home, ".local", "share", "Steam")
	dota := filepath.Join(steam, "steamapps", "common", "dota 2 beta", "game", "dota")
	if err := os.MkdirAll(dota, 0o755); err != nil {
		t.Fatal(err)
	}
	// ~/.steam/steam is a symlink to the same Steam, which must not count twice.
	os.MkdirAll(filepath.Join(home, ".steam"), 0o755)
	os.Symlink(steam, filepath.Join(home, ".steam", "steam"))
	found := FindDota()
	if len(found) != 1 || !strings.HasPrefix(found[0], steam) {
		t.Fatalf("found = %v", found)
	}
}

func TestFindDotaInFlatpakSteam(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Linux Steam layout")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("WSL_DISTRO_NAME", "")
	steam := filepath.Join(home, ".var", "app", "com.valvesoftware.Steam", ".local", "share", "Steam")
	os.MkdirAll(filepath.Join(steam, "steamapps", "common", "dota 2 beta", "game", "dota"), 0o755)
	if found := FindDota(); len(found) != 1 {
		t.Fatalf("found = %v", found)
	}
}
