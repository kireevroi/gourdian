package server

import (
	"maps"
	"net/http"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"time"

	"gourdian/internal/autostart"
	"gourdian/internal/hud"
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
		http.Error(w, err.Error(), http.StatusInternalServerError)
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
		"data": s.workDir, "stats": s.stats.Dir(), "recordings": s.recordingsDir(), "logs": filepath.Join(s.workDir, "logs"),
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
		http.Error(w, err.Error(), http.StatusInternalServerError)
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
	s.overlayMu.Lock()
	if body.HotkeyProblems != nil || body.HUDError == nil {
		s.hotkeyProblems = body.HotkeyProblems
	}
	if body.HUDError != nil {
		s.hudError, s.hudReported = *body.HUDError, true
	}
	s.overlayMu.Unlock()
	s.publishSettings()
	writeJSON(w, map[string]string{"status": "ok"})
}

// hudStatus is whether the Linux HUD has reported in, and its error if it couldn't open.
func (s *Server) hudStatus() (reported bool, err string) {
	s.overlayMu.Lock()
	defer s.overlayMu.Unlock()
	return s.hudReported, s.hudError
}

func (s *Server) hotkeyProblemsCopy() map[string]string {
	s.overlayMu.Lock()
	defer s.overlayMu.Unlock()
	return maps.Clone(s.hotkeyProblems)
}

func (s *Server) hudPayload() hud.Payload {
	set := s.cfg.Settings()
	snap, tips := s.snapshot(set), s.engine.RecentTips()
	s.hudMu.Lock()
	live := hud.BuildHeld(snap, tips, set.HUDWidgets, time.Now(), &s.hudQueue, set.Language)
	s.hudMu.Unlock()
	return hud.Payload{Live: live, Sample: hud.SampleIn(set.HUDWidgets, set.Language)}
}

func (s *Server) handleHUD(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.hudPayload())
}
