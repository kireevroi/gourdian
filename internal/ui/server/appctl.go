package server

import (
	"net/http"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"

	"gourdian/internal/sys/autostart"
	"gourdian/internal/ui/hud"
)

// heroNames names the heroes the player has a remembered role for.
func (s *Server) heroNames() map[string]string {
	names := map[string]string{}
	for id := range s.cfg.Settings().HeroRoles {
		n, err := strconv.Atoi(id)
		if err != nil || s.data == nil {
			names[id] = "hero " + id
			continue
		}
		names[id] = s.data.HeroName(n)
	}
	return names
}

func (s *Server) handleAutostart(w http.ResponseWriter, r *http.Request) {
	var body struct {
		On bool `json:"on"`
	}
	if err := readJSON(w, r, 1<<10, &body); err != nil {
		http.Error(w, `send {"on": true} or {"on": false}`, http.StatusBadRequest)
		return
	}
	if err := autostart.Set(body.On); err != nil {
		s.failed(w, http.StatusInternalServerError, "couldn't change whether the trainer starts with your session", err)
		return
	}
	resp := s.settingsResponse()
	s.hub.publish("settings", resp)
	writeJSON(w, resp)
}

// handleExportCSV writes every table to CSV files, for opening in a spreadsheet.
func (s *Server) handleExportCSV(w http.ResponseWriter, r *http.Request) {
	dir, err := s.stats.Export()
	if err != nil {
		http.Error(w, "couldn't write the CSV files: "+err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]string{"dir": dir, "status": "CSV files written to " + dir})
}

// handleOpenFolder opens one of the app's data folders in Explorer.
func (s *Server) handleOpenFolder(w http.ResponseWriter, r *http.Request) {
	folders := map[string]string{
		"data": s.workDir, "stats": s.stats.Dir(), "recordings": s.rec.Dir, "logs": filepath.Join(s.workDir, "logs"),
	}
	dir, ok := folders[r.PathValue("name")]
	switch {
	case !ok:
		http.Error(w, "unknown folder", http.StatusNotFound)
		return
	}
	opener := exec.Command("explorer.exe", dir)
	if runtime.GOOS != "windows" {
		opener = exec.Command("xdg-open", dir)
	}
	if err := opener.Start(); err != nil {
		s.failed(w, http.StatusInternalServerError, "couldn't open that folder", err)
		return
	}
	writeJSON(w, map[string]string{"opened": dir})
}

// handleHUDEdit toggles the in-game HUD layout editor, like its hotkey.
func (s *Server) handleHUDEdit(w http.ResponseWriter, r *http.Request) {
	s.hub.publish("hud_edit", true)
	writeJSON(w, map[string]string{"status": "Switch to Dota: the HUD can now be dragged. Click Done on it when finished."})
}

// handleOverlayStatus stores problems the overlay reports, such as hotkeys other programs use.
func (s *Server) handleOverlayStatus(w http.ResponseWriter, r *http.Request) {
	var body struct {
		HotkeyProblems map[string]string `json:"hotkey_problems"`
		// HUDError is set by the Linux HUD: empty once its window is up, else why it isn't.
		HUDError *string `json:"hud_error"`
	}
	if err := readJSON(w, r, 8<<10, &body); err != nil {
		http.Error(w, "bad status", http.StatusBadRequest)
		return
	}
	s.overlay.set(body.HotkeyProblems, body.HUDError)
	s.publishSettings()
	writeJSON(w, map[string]string{"status": "ok"})
}

func (s *Server) hudPayload() hud.Payload {
	set := s.cfg.Settings()
	snap, tips := s.snapshot(set), s.engine.RecentTips()
	live := s.alerts.build(snap, tips, set)
	return hud.Payload{Live: live, Sample: hud.SampleIn(set.HUDWidgets, set.Language),
		// Choosing ends with the player's own pick; reading the screen lasts the whole draft,
		// since the other side is still picking and what is known of them would go stale.
		Choosing:   pickMatters(snap),
		Draft:      set.Screen.Draft && draftMatters(snap),
		KeepFrames: set.Screen.Draft && set.Screen.KeepFrames}
}

func (s *Server) handleHUD(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.hudPayload())
}
