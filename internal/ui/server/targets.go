package server

import (
	"gourdian/internal/data/stats"
	"gourdian/internal/game/model"
)

// targetHistory spells the query so internal/targets doesn't have to know how matches are stored.
type targetHistory struct{ *stats.Store }

func (h targetHistory) RecentOn(heroID int, role string, limit int) ([]model.MatchSummary, error) {
	return h.MatchesWhere(stats.MatchFilter{HeroID: heroID, Role: role, Real: true, Limit: limit})
}
