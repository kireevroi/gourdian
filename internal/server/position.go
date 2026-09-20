package server

import (
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"gourdian/internal/coach"
	"gourdian/internal/config"
	"gourdian/internal/dota"
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
	set, err := s.cfg.Update(func(cur *config.Settings) error {
		cur.Role = res.DetectedRole
		return nil
	})
	if err != nil {
		s.log.Error("apply detected role", "err", err)
		return set
	}
	lang := set.Language
	s.engine.SetRoleNote(roleSay(lang, "you laned %s", laneIn(lang, res.DetectedLane)))
	s.applyFocus(set.Role, s.engine.HeroID())
	s.publishSettingsLater()
	s.log.Info("role detected from laning", "lane", res.DetectedLane, "role", set.Role)
	snap := s.engine.Snapshot(set)
	say := func(lang string) (string, string) {
		lane, name := laneIn(lang, res.DetectedLane), dota.RoleName(set.Role, lang)
		return roleSay(lang, "You're laning %s, so the coach switched you to %s. Another position? Press Ctrl+Shift+1 to 5", lane, name),
			roleSay(lang, "You're %s. Coaching you as %s.", lane, name)
	}
	tip := coach.Tip{Rule: "role_check", Category: "focus", Severity: coach.Info, Clock: snap.Clock, At: time.Now()}
	tip.Text, tip.Speech = say(lang)
	if lang != "en" {
		_, tip.SpeechEN = say("en")
	}
	s.emitTips(matchID, []coach.Tip{tip}, set)
	return set
}

// handleRole sets the position the player is playing, from the in-game hotkeys or the dashboard.
func (s *Server) handleRole(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Role string `json:"role"`
	}
	if err := readJSON(w, r, 1<<10, &body); err != nil || !slices.Contains(dota.Roles, body.Role) {
		http.Error(w, `send {"role": "carry"}, or mid, offlane, soft_support, hard_support`, http.StatusBadRequest)
		return
	}
	changed := false
	set, err := s.cfg.Update(func(cur *config.Settings) error {
		changed = cur.Role != body.Role
		cur.Role = body.Role
		return nil
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if snap := s.engine.Snapshot(set); snap.InMatch && snap.Hero != nil && !strings.HasPrefix(snap.MatchID, "sim-") {
		s.lockRole(snap.MatchID)
		s.rememberHeroRole(snap.Hero.ID, set.Role)
		s.engine.SetRoleNote(roleSay(set.Language, "your pick"))
		s.applyFocus(set.Role, snap.Hero.ID)
		if changed {
			name := dota.RoleName(set.Role, set.Language)
			tip := coach.Tip{Rule: "role_pick", Category: "focus", Severity: coach.Info, Clock: snap.Clock, At: time.Now(),
				Text: roleSay(set.Language, "Coaching you as %s", name), Speech: roleSay(set.Language, "Coaching you as %s.", name)}
			if set.Language != "en" {
				tip.SpeechEN = roleSay("en", "Coaching you as %s.", dota.RoleName(set.Role, "en"))
			}
			s.emitTips(snap.MatchID, []coach.Tip{tip}, set)
		}
	}
	resp := s.settingsResponse()
	s.hub.publish("settings", resp)
	s.dirty.Store(true)
	writeJSON(w, resp)
}

func (s *Server) rememberHeroRole(heroID int, role string) {
	key := strconv.Itoa(heroID)
	if heroID == 0 || s.cfg.Settings().HeroRoles[key] == role {
		return
	}
	if _, err := s.cfg.Update(func(set *config.Settings) error {
		if set.HeroRoles == nil {
			set.HeroRoles = map[string]string{}
		}
		set.HeroRoles[key] = role
		return nil
	}); err != nil {
		s.log.Error("remember hero role", "err", err)
	}
}

// applyHeroRole picks the role when a match starts on a different hero. The pre-game
// role line tells the player where the choice came from.
func (s *Server) applyHeroRole(heroID int, set config.Settings) config.Settings {
	// The lock covers the choice and its settings write, so a post about an older hero can't
	// write its role over a newer hero's. The dashboard update runs without it.
	s.roleMu.Lock()
	defer s.roleMu.Unlock()
	if s.roleHero == heroID {
		return set
	}
	s.roleHero = heroID
	role, note := s.roleFor(heroID, set)
	s.engine.SetRoleNote(note)
	defer func() { s.applyFocus(s.cfg.Settings().Role, heroID) }()
	if role == "" {
		return set
	}
	// Compared with the settings now, not set: another post may have changed them since.
	changed := false
	set, err := s.cfg.Update(func(cur *config.Settings) error {
		changed = cur.Role != role
		cur.Role = role
		return nil
	})
	if err != nil {
		s.log.Error("apply hero role", "err", err)
		return set
	}
	if !changed {
		return set
	}
	s.publishSettingsLater()
	s.log.Info("role set for hero", "role", role, "why", note)
	return set
}

// roleFor tries the role last played on the hero, then the player's most common role on it
// (imported matches count), then the hero's own roles from OpenDota.
func (s *Server) roleFor(heroID int, set config.Settings) (role, note string) {
	name := fmt.Sprintf("hero %d", heroID)
	if s.data != nil {
		name = s.data.HeroName(heroID)
	}
	if r, ok := set.HeroRoles[strconv.Itoa(heroID)]; ok {
		return r, roleSay(set.Language, "what you played on %s last time", name)
	}
	if r := s.usualRole(heroID); r != "" {
		return r, roleSay(set.Language, "your usual role on %s", name)
	}
	if s.data != nil {
		if info, ok := s.data.Hero(heroID); ok {
			if r := roleFromHeroRoles(info.Roles); r != "" {
				return r, roleSay(set.Language, "a guess for %s", name)
			}
		}
	}
	return "", ""
}

func (s *Server) usualRole(heroID int) string {
	role, err := s.stats.UsualRole(heroID, dota.Roles)
	if err != nil {
		s.log.Warn("read the usual role", "hero", heroID, "err", err)
	}
	return role
}

// roleFromHeroRoles reads OpenDota's hero roles, which list the main ones first.
func roleFromHeroRoles(roles []string) string {
	support, carry := slices.Index(roles, "Support"), slices.Index(roles, "Carry")
	switch {
	case support >= 0 && (carry < 0 || support < carry):
		return dota.SoftSupport
	case carry >= 0:
		return dota.Carry
	}
	return ""
}
