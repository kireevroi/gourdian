//go:build !windows

package install

import (
	"os/exec"
	"strings"

	"gourdian/internal/sys/platform"
)

// registeredSteamRoot is the folder the Windows Steam says it is installed in, read through
// reg.exe when running under WSL, or "" elsewhere.
func registeredSteamRoot() string {
	if !platform.WSL() {
		return ""
	}
	regExe, err := exec.LookPath("reg.exe")
	if err != nil {
		return ""
	}
	cmd := exec.Command(regExe, "query", `HKCU\Software\Valve\Steam`, "/v", "SteamPath")
	cmd.Dir = "/mnt/c" // Windows programs refuse to start in a Linux working directory
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return WSLPath(regValue(string(out), "SteamPath"))
}

// regValue finds a REG_SZ value in `reg query` output.
func regValue(out, name string) string {
	for _, line := range strings.Split(strings.ReplaceAll(out, "\r", ""), "\n") {
		fields := strings.SplitN(strings.TrimSpace(line), "REG_SZ", 2)
		if len(fields) == 2 && strings.TrimSpace(fields[0]) == name {
			return strings.TrimSpace(fields[1])
		}
	}
	return ""
}
