package config

import (
	"path/filepath"

	"golang.org/x/sys/windows/registry"
)

// registeredInstallDir is the folder the installer put the app in, or "".
func registeredInstallDir() string {
	key, err := registry.OpenKey(registry.CURRENT_USER, uninstallKey, registry.QUERY_VALUE)
	if err != nil {
		return ""
	}
	defer key.Close()
	dir, _, err := key.GetStringValue("InstallLocation")
	if err != nil || dir == "" {
		return ""
	}
	return filepath.Clean(dir)
}
