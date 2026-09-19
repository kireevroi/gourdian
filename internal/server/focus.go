package server

import (
	"gourdian/internal/coach"
	"strings"

	"gourdian/internal/config"
	"gourdian/internal/stats"
)

// applyFocus picks the focus for the match about to start: this hero and position first,
// then this position on any hero, then the last review of any game.
func (s *Server) applyFocus(role string, heroID int) {
	reviews, err := s.stats.Reviews()
	if err != nil || len(reviews) == 0 {
		return
	}
	var sameHero, sameRole, any stats.Review
	for _, r := range reviews {
		if strings.TrimSpace(r.NextGameFocus) == "" {
			continue
		}
		any = r
		if r.Role == role || r.Role == "" {
			sameRole = r
		}
		if r.Role == role && heroID > 0 && r.HeroID == heroID {
			sameHero = r
		}
	}
	switch {
	case sameHero.NextGameFocus != "":
		s.engine.SetFocus(sameHero.NextGameFocus)
	case sameRole.NextGameFocus != "":
		s.engine.SetFocus(sameRole.NextGameFocus)
	case any.NextGameFocus != "":
		s.engine.SetFocus(fromRole(any, s.cfg.Settings().Language))
	}
}

// lastFocusFor is the focus the previous review set for this position, so the next review
// can check whether it happened.
func (s *Server) lastFocusFor(role string, heroID int) string {
	reviews, err := s.stats.Reviews()
	if err != nil {
		return ""
	}
	best := ""
	for _, r := range reviews {
		if strings.TrimSpace(r.NextGameFocus) == "" {
			continue
		}
		if r.Role == role || r.Role == "" && heroID == r.HeroID {
			best = r.NextGameFocus
		}
	}
	return best
}

// fromRole says which game a focus came from when it wasn't this position.
func fromRole(r stats.Review, lang string) string {
	name := coach.RoleName(r.Role, lang)
	switch {
	case name != "" && r.Hero != "":
		return roleSay(lang, "%s (from your %s game as %s)", r.NextGameFocus, r.Hero, name)
	case r.Hero != "":
		return roleSay(lang, "%s (from your %s game)", r.NextGameFocus, r.Hero)
	}
	return r.NextGameFocus
}

// reviewRole is the position played in a match, for storing with its review.
func reviewRole(m stats.MatchSummary, set config.Settings) string {
	if m.Role != "" {
		return m.Role
	}
	return set.Role
}
