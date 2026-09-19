package server

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"time"

	"gourdian/internal/coach"
	"gourdian/internal/config"
)

// lockRole records that the player picked the role for this match, so lane detection leaves it alone.
func (s *Server) lockRole(matchID string) {
	s.roleMu.Lock()
	s.roleLock = matchID
	s.roleMu.Unlock()
}

func (s *Server) roleLocked(matchID string) bool {
	s.roleMu.Lock()
	defer s.roleMu.Unlock()
	return matchID != "" && s.roleLock == matchID
}

// applyDetectedRole switches to the position the hero laned in, unless the player chose one.
func (s *Server) applyDetectedRole(res coach.Result, matchID string, set config.Settings) config.Settings {
	if res.DetectedRole == "" || s.roleLocked(matchID) {
		return set
	}
	set.Role = res.DetectedRole
	if err := s.cfg.UpdateSettings(set); err != nil {
		s.log.Error("apply detected role", "err", err)
		return s.cfg.Settings()
	}
	lang := set.Language
	s.engine.SetRoleNote(roleSay(lang, "you laned %s", laneIn(lang, res.DetectedLane)))
	s.applyFocus(set.Role, s.engine.HeroID())
	s.hub.publish("settings", s.settingsResponse())
	s.log.Info("role detected from laning", "lane", res.DetectedLane, "role", set.Role)
	snap := s.engine.Snapshot(set)
	say := func(lang string) (string, string) {
		lane, name := laneIn(lang, res.DetectedLane), coach.RoleName(set.Role, lang)
		return roleSay(lang, "You're laning %s, so the coach switched you to %s. Another position? Press Ctrl+Shift+1 to 5", lane, name),
			roleSay(lang, "You're %s. Coaching you as %s.", lane, name)
	}
	tip := coach.Tip{Rule: "role_check", Category: "focus", Severity: coach.Info, Clock: snap.Clock, At: time.Now()}
	tip.Text, tip.Speech = say(lang)
	if lang != "en" {
		_, tip.SpeechEN = say("en")
	}
	s.engine.AddTips([]coach.Tip{tip})
	s.deliver(matchID, []coach.Tip{tip}, set)
	return set
}

// handleRole sets the position the player is playing, from the in-game hotkeys or the dashboard.
func (s *Server) handleRole(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Role string `json:"role"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<10)).Decode(&body); err != nil || !slices.Contains(config.Roles, body.Role) {
		http.Error(w, `send {"role": "carry"}, or mid, offlane, soft_support, hard_support`, http.StatusBadRequest)
		return
	}
	set := s.cfg.Settings()
	changed := set.Role != body.Role
	set.Role = body.Role
	if err := s.cfg.UpdateSettings(set); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if snap := s.engine.Snapshot(set); snap.InMatch && snap.Hero != nil && !strings.HasPrefix(snap.MatchID, "sim-") {
		s.lockRole(snap.MatchID)
		s.rememberHeroRole(snap.Hero.ID, set.Role)
		s.engine.SetRoleNote(roleSay(set.Language, "your pick"))
		s.applyFocus(set.Role, snap.Hero.ID)
		if changed {
			name := coach.RoleName(set.Role, set.Language)
			tip := coach.Tip{Rule: "role_pick", Category: "focus", Severity: coach.Info, Clock: snap.Clock, At: time.Now(),
				Text: roleSay(set.Language, "Coaching you as %s", name), Speech: roleSay(set.Language, "Coaching you as %s.", name)}
			if set.Language != "en" {
				tip.SpeechEN = roleSay("en", "Coaching you as %s.", coach.RoleName(set.Role, "en"))
			}
			s.engine.AddTips([]coach.Tip{tip})
			s.deliver(snap.MatchID, []coach.Tip{tip}, set)
		}
	}
	resp := s.settingsResponse()
	s.hub.publish("settings", resp)
	s.dirty.Store(true)
	writeJSON(w, resp)
}
