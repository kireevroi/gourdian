package app

import (
	"net/http"
	"os/exec"
	"runtime"
	"time"

	"gourdian/internal/hidewin"
	"gourdian/internal/overlay"
)

// trainerAnswers reports whether a trainer is already serving the dashboard at url.
func trainerAnswers(url string) bool {
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(url + "/api/state")
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch {
	case runtime.GOOS == "windows":
		overlay.OpenDashboard(url, true)
		return
	case runtime.GOOS == "darwin":
		cmd = exec.Command("open", url)
	default:
		if exe, err := exec.LookPath("cmd.exe"); err == nil {
			cmd = hidewin.WindowsCommand(exe, "/c", "start", url)
		} else {
			cmd = exec.Command("xdg-open", url)
		}
	}
	_ = cmd.Start()
}
