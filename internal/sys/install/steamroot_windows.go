package install

import (
	"path/filepath"

	"golang.org/x/sys/windows/registry"
)

// registeredSteamRoot is the folder Steam says it is installed in, or "". Steam can live on
// any drive, so the usual Program Files folders aren't enough.
func registeredSteamRoot() string {
	for _, k := range []struct {
		root       registry.Key
		path, name string
	}{
		{registry.CURRENT_USER, `Software\Valve\Steam`, "SteamPath"},
		{registry.LOCAL_MACHINE, `SOFTWARE\WOW6432Node\Valve\Steam`, "InstallPath"},
	} {
		key, err := registry.OpenKey(k.root, k.path, registry.QUERY_VALUE)
		if err != nil {
			continue
		}
		dir, _, err := key.GetStringValue(k.name)
		key.Close()
		if err == nil && dir != "" {
			return filepath.Clean(dir)
		}
	}
	return ""
}
