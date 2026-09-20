package server

import (
	"net/http"
	"os"
	"slices"
	"strings"

	"gourdian/internal/config"
	"gourdian/internal/install"
	"gourdian/internal/platform"
)

// setupCheck is one line of the dashboard's setup card, shown until Dota sends data.
type setupCheck struct {
	Label  string `json:"label"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail,omitempty"`
	// Fix names a button the dashboard can offer: "install_gsi".
	Fix string `json:"fix,omitempty"`
}

func (s *Server) setupChecks() []setupCheck {
	cfg := s.cfg.Get()
	dirs := install.FindDota()
	if len(dirs) == 0 {
		return []setupCheck{{Label: "Dota 2 not found", Detail: "Dota 2 wasn't found in your Steam libraries. Install it through Steam, then restart Gourdian."}}
	}
	var checks []setupCheck
	want := install.Render(config.GSIURI(cfg.Listen), cfg.Token)
	for _, d := range dirs {
		if got, err := os.ReadFile(install.CfgPath(d)); err == nil && string(got) == want {
			checks = append(checks, setupCheck{Label: "Dota is set up to send game data", OK: true})
		} else {
			checks = append(checks, setupCheck{Label: "Dota isn't set up to send game data yet",
				Detail: "Gourdian normally does this when it starts.", Fix: "install_gsi"})
		}
		switch mode, err := install.VideoMode(d); {
		case err != nil:
		case mode == install.Exclusive:
			checks = append(checks, setupCheck{Label: "Dota runs in exclusive fullscreen",
				Detail: "The HUD can't draw over it. In Dota: Settings › Video › Display mode › Borderless window."})
		default:
			checks = append(checks, setupCheck{Label: "Dota's display mode lets the HUD show (" + string(mode) + ")", OK: true})
		}
	}
	if s.speaker != nil && !platform.LinuxDesktop() && cfg.Settings.Language != "en" && cfg.Settings.Voice == config.VoiceSystem {
		if langs := s.speaker.Languages(); langs != nil && !slices.Contains(langs, cfg.Settings.Language) {
			checks = append(checks, setupCheck{Label: "Windows has no Russian voice, so tips are spoken in English",
				Detail: "Install it here (Windows asks for permission), or in Windows Settings › Time & language › Speech › Add voices › Russian.", Fix: "install_voice"})
		}
	}
	if platform.LinuxDesktop() {
		steam := "Or show the HUD in the Steam overlay: press Shift+Tab, open the web browser, go to " + config.DashboardHost(cfg.Listen) + "/overlay.html and pin it."
		switch reported, hudErr := s.hudStatus(); {
		case hudErr != "":
			checks = append(checks, setupCheck{Label: "The HUD can't show over Dota", Detail: hudErr + ". " + steam})
		case reported:
			checks = append(checks, setupCheck{Label: "The HUD shows over Dota", OK: true, Detail: steam})
		}
		if s.speaker == nil {
			checks = append(checks, setupCheck{Label: "Nothing can read tips aloud",
				Detail: "Install PipeWire (pw-play) or alsa-utils (aplay) for the natural voice, then restart the trainer."})
		}
		checks = append(checks, s.piperChecks(cfg.Settings)...)
	}
	if opts := install.LaunchOptions(); len(opts) > 0 {
		ok := true
		for _, o := range opts {
			ok = ok && strings.Contains(o, "-gamestateintegration")
		}
		if ok {
			checks = append(checks, setupCheck{Label: "Launch options include -gamestateintegration", OK: true})
		} else {
			checks = append(checks, setupCheck{Label: "Launch options are missing -gamestateintegration",
				Detail: "In Steam: right-click Dota 2 › Properties › Launch options, add -gamestateintegration."})
		}
	}
	return checks
}

func (s *Server) handleSetup(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.setupChecks())
}

// handleSetupInstall writes Dota's game-state config, for when it's missing or out of date.
func (s *Server) handleSetupInstall(w http.ResponseWriter, r *http.Request) {
	cfg := s.cfg.Get()
	dirs := install.FindDota()
	if len(dirs) == 0 {
		http.Error(w, "Dota 2 not found", http.StatusNotFound)
		return
	}
	for _, d := range dirs {
		if _, err := install.Write(d, config.GSIURI(cfg.Listen), cfg.Token); err != nil {
			http.Error(w, "couldn't write the Dota config: "+err.Error(), http.StatusInternalServerError)
			return
		}
	}
	s.log.Info("installed the Dota game-state config from the dashboard")
	writeJSON(w, s.setupChecks())
}
