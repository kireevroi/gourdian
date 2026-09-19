//go:build linux

package overlay

import "os/exec"

// appBrowsers can open the dashboard as an app window, without tabs or an address bar.
var appBrowsers = []string{"google-chrome", "google-chrome-stable", "chromium", "chromium-browser", "brave", "brave-browser",
	"microsoft-edge", "microsoft-edge-stable", "vivaldi"}

func ShellOpen(target string) { _ = exec.Command("xdg-open", target).Start() }

// OpenDashboard opens the dashboard in an app window of a Chromium browser when appWindow is
// set and one is installed, otherwise in the default browser.
func OpenDashboard(url string, appWindow bool) {
	if appWindow {
		for _, name := range appBrowsers {
			if exe, err := exec.LookPath(name); err == nil && exec.Command(exe, "--app="+url).Start() == nil {
				return
			}
		}
	}
	ShellOpen(url)
}

// ShowMessage shows a desktop notification, where the desktop has notify-send.
func ShowMessage(title, text string, isError bool) {
	exe, err := exec.LookPath("notify-send")
	if err != nil {
		return
	}
	args := []string{"--app-name=Dota Trainer"}
	if isError {
		args = append(args, "--urgency=critical")
	}
	_ = exec.Command(exe, append(args, title, text)...).Start()
}
