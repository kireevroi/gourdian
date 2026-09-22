package server

import (
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"gourdian/internal/coaching/coach"
	"gourdian/internal/coaching/position"
	"gourdian/internal/game/dota"
	"gourdian/internal/i18n"
	"gourdian/internal/sys/config"
)

// applyDetectedRole switches to the position the hero laned in, unless the player chose one.
func (s *Server) applyDetectedRole(res coach.Result, matchID string, set config.Settings) config.Settings {
	if res.DetectedRole == "" || s.role.Locked(matchID) {
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
	s.role.Guessed(set.Role)
	s.engine.SetRoleNote(i18n.Say(lang, "you laned %s", laneIn(lang, res.DetectedLane)))
	s.applyFocus(set.Role, s.engine.HeroID())
	s.publishSettingsLater()
	s.log.Info("role detected from laning", "lane", res.DetectedLane, "role", set.Role)
	snap := s.engine.Snapshot(set)
	say := func(lang string) (string, string) {
		lane, name := laneIn(lang, res.DetectedLane), dota.RoleName(set.Role, lang)
		return i18n.Say(lang, "You're laning %s, so the coach switched you to %s. Another position? Press Ctrl+Shift+1 to 5", lane, name),
			i18n.Say(lang, "You're %s. Coaching you as %s.", lane, name)
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
		s.settingsProblem(w, err)
		return
	}
	snapNow := s.engine.Snapshot(set)
	// Their answer stands from wherever it was given -- the dashboard while queueing as much
	// as the in-game keys once the draft is open.
	s.role.Own()
	// No hero to remember it against yet, so hold it: when one appears it must beat what they
	// happened to play on that hero last time.
	if pickMatters(snapNow) {
		s.role.Choose(set.Role, time.Now())
		s.engine.SetRoleNote(i18n.Say(set.Language, "your pick"))
		if changed {
			s.sayCoachingAs(snapNow.MatchID, snapNow.Clock, set)
		}
	}
	if snap := snapNow; snap.InMatch && snap.Hero != nil && !strings.HasPrefix(snap.MatchID, "sim-") {
		s.role.Lock(snap.MatchID)
		s.rememberHeroRole(snap.Hero.ID, set.Role)
		s.engine.SetRoleNote(i18n.Say(set.Language, "your pick"))
		s.applyFocus(set.Role, snap.Hero.ID)
		if changed {
			s.sayCoachingAs(snap.MatchID, snap.Clock, set)
		}
	}
	resp := s.settingsResponse()
	s.hub.publish("settings", resp)
	s.dirty.Store(true)
	writeJSON(w, resp)
}

// sayCoachingAs tells the player which position the trainer is now coaching them as.
func (s *Server) sayCoachingAs(matchID string, clock int, set config.Settings) {
	name := dota.RoleName(set.Role, set.Language)
	tip := coach.Tip{Rule: "role_pick", Category: "focus", Severity: coach.Info, Clock: clock, At: time.Now(),
		Text: i18n.Say(set.Language, "Coaching you as %s", name), Speech: i18n.Say(set.Language, "Coaching you as %s.", name)}
	if set.Language != "en" {
		tip.SpeechEN = i18n.Say("en", "Coaching you as %s.", dota.RoleName(set.Role, "en"))
	}
	s.emitTips(matchID, []coach.Tip{tip}, set)
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
	s.role.OnNewHero(heroID, time.Now(), func(chosen string) (guessed string) {
		role, note := s.roleFor(heroID, set)
		// A position chosen while still picking beats anything worked out from the hero: they
		// said which one they are playing, and they said it about this game.
		if chosen != "" {
			role, note = chosen, i18n.Say(set.Language, "your pick")
			defer s.rememberHeroRole(heroID, chosen)
		} else {
			guessed = role
		}
		s.engine.SetRoleNote(note)
		defer func() { s.applyFocus(s.cfg.Settings().Role, heroID) }()
		if role == "" {
			return ""
		}
		// Compared with the settings now, not set: another post may have changed them since.
		changed := false
		var err error
		set, err = s.cfg.Update(func(cur *config.Settings) error {
			changed = cur.Role != role
			cur.Role = role
			return nil
		})
		if err != nil {
			s.log.Error("apply hero role", "err", err)
			return ""
		}
		// Nothing moved, so whoever put that position there still owns it.
		if !changed {
			return ""
		}
		s.publishSettingsLater()
		s.log.Info("role set for hero", "role", role, "why", note)
		return guessed
	})
	return set
}

// roleFor gathers what is known about the hero and lets position.Choose decide.
func (s *Server) roleFor(heroID int, set config.Settings) (role, note string) {
	c := position.Candidates{Remembered: set.HeroRoles[strconv.Itoa(heroID)], Usual: s.usualRole(heroID)}
	name := fmt.Sprintf("hero %d", heroID)
	if s.data != nil {
		name = s.data.HeroName(heroID)
		if info, ok := s.data.Hero(heroID); ok {
			c.HeroRoles = info.Roles
		}
	}
	return position.Choose(c, name, set.Language)
}

func (s *Server) usualRole(heroID int) string {
	role, err := s.stats.UsualRole(heroID, dota.Roles)
	if err != nil {
		s.log.Warn("read the usual role", "hero", heroID, "err", err)
	}
	return role
}
