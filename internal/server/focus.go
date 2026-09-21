package server

import (
	"gourdian/internal/config"
	"gourdian/internal/focus"
	"gourdian/internal/model"
)

// applyFocus tells the engine what to work on in the match about to start.
func (s *Server) applyFocus(role string, heroID int) {
	reviews, err := s.stats.Reviews()
	if err != nil || len(reviews) == 0 {
		return
	}
	if f := focus.Pick(reviews, role, heroID, s.cfg.Settings().Language); f != "" {
		s.engine.SetFocus(f)
	}
}

func (s *Server) lastFocusFor(role string, heroID int) string {
	reviews, err := s.stats.Reviews()
	if err != nil {
		return ""
	}
	return focus.Last(reviews, role, heroID)
}

// reviewRole is the position played in a match, for storing with its review.
func reviewRole(m model.MatchSummary, set config.Settings) string {
	if m.Role != "" {
		return m.Role
	}
	return set.Role
}
