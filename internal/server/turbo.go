package server

import (
	"context"
	"time"

	"gourdian/internal/model"
	"gourdian/internal/stats"
)

// backfillWindow is how far back matches are looked up to find out what mode they were. It is
// the span the trainer works its targets out over, so older ones make no difference to
// anything a player sees.
const backfillWindow = 120 * 24 * time.Hour

// learnGameModes asks OpenDota what mode each recent match was, for the ones recorded before
// the trainer knew to ask.
//
// Only matches the trainer watched need it: imported history comes from OpenDota's own list
// of significant matches, which leaves Turbo out already. Without this, a player's targets
// stay pulled towards Turbo numbers until every such match has aged out of the window.
func (s *Server) learnGameModes(ctx context.Context) {
	matches, err := s.stats.MatchesWhere(stats.MatchFilter{Since: time.Now().Add(-backfillWindow), Real: true, Turbo: true})
	if err != nil {
		s.log.Warn("couldn't look over the match history for its game modes", "err", err)
		return
	}
	asked, found := 0, 0
	for _, m := range matches {
		if ctx.Err() != nil {
			return
		}
		// A mode of 0 means nobody has said yet. Matches the trainer didn't watch came from
		// OpenDota already filtered, and a match without a real id can't be looked up.
		if m.GameMode != 0 || m.Source != model.SourceLive || !numeric(m.MatchID) {
			continue
		}
		asked++
		match, err := s.data.Match(ctx, m.MatchID)
		if err != nil {
			continue
		}
		if err := s.stats.UpdateMatch(m.MatchID, func(row *model.MatchSummary) { row.GameMode = match.GameMode }); err != nil {
			s.log.Warn("couldn't record a match's game mode", "match", m.MatchID, "err", err)
			continue
		}
		if match.GameMode == model.GameModeTurbo {
			found++
		}
	}
	if asked > 0 {
		s.log.Info("learned what mode the older matches were", "asked", asked, "turbo", found)
	}
}

func numeric(id string) bool {
	if id == "" {
		return false
	}
	for _, r := range id {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
