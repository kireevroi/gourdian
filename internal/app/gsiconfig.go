package app

import (
	"log/slog"
	"os"

	"gourdian/internal/config"
	"gourdian/internal/install"
)

// EnsureInstalled writes or repairs the GSI config so a fresh install works without extra steps.
func EnsureInstalled(cfg config.Config, log *slog.Logger) {
	if config.HomeOverride() != "" {
		// Test profiles must not point Dota at themselves; `gourdian install` still can.
		return
	}
	dirs := install.FindDota()
	if len(dirs) == 0 {
		log.Warn("Dota 2 install not found; if it's in a custom location run `gourdian install -dota DIR`")
		return
	}
	want := install.Render(config.GSIURI(cfg.Listen), cfg.Token)
	for _, d := range dirs {
		if got, err := os.ReadFile(install.CfgPath(d)); err == nil && string(got) == want {
			continue
		}
		if _, err := install.Write(d, config.GSIURI(cfg.Listen), cfg.Token); err != nil {
			log.Error("couldn't install the GSI config; run `gourdian install`", "dota", d, "err", err)
			continue
		}
		log.Warn("installed the Dota 2 game-state config; restart Dota 2 if it's already running", "dota", d)
	}
}
