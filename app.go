package main

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"dotatrainer/internal/buildinfo"
	"dotatrainer/internal/config"
	"dotatrainer/internal/overlay"
)

// setupCmd prepares the installed app folder: config with its auth token, data folders and
// the Dota 2 game-state config. The installer runs it after copying files.
func setupCmd() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	dir := filepath.Dir(exe)
	for _, sub := range []string{"stats", "recordings", "logs", "cache"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
			return err
		}
	}
	store, err := config.Open(dir)
	if err != nil {
		return err
	}
	ensureInstalled(store.Get(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	return nil
}

// trainerCall sends a request to a running trainer. Under WSL, localhost doesn't reach apps
// running on Windows, so it falls back to Windows' curl.exe.
func trainerCall(method, url string) bool {
	_, ok := trainerFetch(method, url)
	return ok
}

// trainerFetch is trainerCall that also returns the response body.
func trainerFetch(method, url string) ([]byte, bool) {
	client := &http.Client{Timeout: 5 * time.Second}
	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		return nil, false
	}
	if resp, err := client.Do(req); err == nil {
		defer resp.Body.Close()
		body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		return body, err == nil && resp.StatusCode < 300
	}
	curl, err := exec.LookPath("curl.exe")
	if err != nil || runtime.GOOS == "windows" {
		return nil, false
	}
	out, err := windowsCommand(curl, "-s", "-f", "-m", "5", "-X", method, url).Output()
	return out, err == nil
}

// stopRunningTrainer asks a running trainer to quit and waits until it has, so its exe can be replaced.
func stopRunningTrainer() {
	store, _, err := openStore()
	if err != nil {
		return
	}
	base := "http://" + config.DashboardHost(store.Get().Listen)
	if !trainerCall("POST", base+"/api/quit") {
		return
	}
	for range 20 {
		time.Sleep(500 * time.Millisecond)
		if !trainerCall("GET", base+"/api/state") {
			time.Sleep(time.Second) // let Windows release the exe file
			return
		}
	}
}

// runApp is what starting the exe without a command does: tray, HUD and dashboard, with errors
// shown as dialogs. background is set when Windows starts it at sign-in.
func runApp(background bool) {
	if store, _, err := openStore(); err == nil {
		base := "http://" + config.DashboardHost(store.Get().Listen)
		client := &http.Client{Timeout: time.Second}
		if resp, err := client.Get(base + "/api/state"); err == nil {
			resp.Body.Close()
			if !background {
				overlay.ShowMessage(config.AppName, "Dota Trainer is already running.\n\nUse its tray icon, or press Ctrl+Shift+F10 in game.", false)
			}
			return
		}
	}
	args := []string{"-overlay", "-tray", "-open-first"}
	if background {
		args = []string{"-overlay", "-tray", "-background"}
	}
	if err := run(args); err != nil {
		overlay.ShowMessage(config.AppName, "Dota Trainer stopped:\n\n"+err.Error(), true)
		os.Exit(1)
	}
}

func versionCmd() error {
	fmt.Println(config.AppName, buildinfo.Version)
	return nil
}
