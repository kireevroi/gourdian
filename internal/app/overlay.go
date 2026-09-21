package app

import (
	"context"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"gourdian/internal/sys/platform"
	"gourdian/internal/ui/overlay"
)

// startOverlay runs the overlay in-process on Windows and Linux desktops; under WSL it launches
// the Windows build sitting next to this binary, since only a Windows process can draw over the game.
func startOverlay(ctx context.Context, o overlay.Options, stop func(), log *slog.Logger) {
	if platform.LinuxDesktop() {
		go func() {
			// The trainer still coaches by voice and the dashboard without a HUD.
			if err := overlay.Run(ctx, o); err != nil {
				log.Error("HUD stopped", "err", err)
			}
		}()
		return
	}
	if runtime.GOOS == "windows" {
		go func() {
			if err := overlay.Run(ctx, o); err != nil {
				log.Error("overlay stopped", "err", err)
			}
			// Its windows also close when Windows or an installer asks the app to exit; exit with them.
			stop()
		}()
		return
	}
	self, err := os.Executable()
	if err != nil {
		log.Error("overlay: locate executable", "err", err)
		return
	}
	exe := filepath.Join(filepath.Dir(self), "gourdian.exe")
	if _, err := os.Stat(exe); err != nil {
		log.Warn("overlay needs the Windows build next to this binary; run `make`", "missing", exe)
		return
	}
	cmd := exec.CommandContext(ctx, exe, "overlay", "-url", o.URL, "-corner", o.Corner)
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		log.Error("overlay: start", "err", err)
		return
	}
	log.Info("overlay started")
	go cmd.Wait()
}
