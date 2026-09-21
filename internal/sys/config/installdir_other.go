//go:build !windows

package config

import (
	"gourdian/internal/sys/platform"
	"os/exec"
	"strings"
)

// registeredInstallDir is the folder the Windows installer put the app in, read through
// reg.exe when running under WSL, or "" elsewhere.
func registeredInstallDir() string {
	if !platform.WSL() {
		return ""
	}
	regExe, err := exec.LookPath("reg.exe")
	if err != nil {
		return ""
	}
	cmd := exec.Command(regExe, "query", `HKCU\`+uninstallKey, "/v", "InstallLocation")
	cmd.Dir = "/mnt/c" // Windows programs refuse to start in a Linux working directory
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return parseRegValue(string(out), "InstallLocation")
}

// parseRegValue finds a REG_SZ value in `reg query` output and returns it as a /mnt path.
func parseRegValue(out, name string) string {
	for _, line := range strings.Split(strings.ReplaceAll(out, "\r", ""), "\n") {
		fields := strings.SplitN(strings.TrimSpace(line), "REG_SZ", 2)
		if len(fields) == 2 && strings.TrimSpace(fields[0]) == name {
			return mntPath(strings.TrimRight(strings.TrimSpace(fields[1]), `\`))
		}
	}
	return ""
}
