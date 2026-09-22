package server

import (
	"context"
	"strconv"
	"time"

	"gourdian/internal/data/opendota"
	"gourdian/internal/data/stats"
	"gourdian/internal/game/model"
)

// learnLastHitCurves gives parsed matches imported before curves were kept OpenDota's per-minute
// last hits, from the copy of each match the client already has on disk.
func (s *Server) learnLastHitCurves(ctx context.Context) {
	matches, err := s.stats.MatchesWhere(stats.MatchFilter{Since: time.Now().Add(-backfillWindow), Real: true, Turbo: true})
	if err != nil {
		s.log.Warn("couldn't look over the match history for last-hit curves", "err", err)
		return
	}
	acct, _ := strconv.ParseInt(s.cfg.Settings().AccountID, 10, 64)
	filled := 0
	for _, m := range matches {
		if ctx.Err() != nil {
			return
		}
		if !m.Parsed || len(m.LastHitsByMinute) > 0 || !numeric(m.MatchID) {
			continue
		}
		match, err := s.data.Match(ctx, m.MatchID)
		if err != nil {
			continue
		}
		d, ok := opendota.Extract(match, acct, m.HeroID)
		if !ok || len(d.LastHitsByMinute) == 0 {
			continue
		}
		if err := s.stats.UpdateMatch(m.MatchID, func(row *model.MatchSummary) { row.LastHitsByMinute = d.LastHitsByMinute }); err != nil {
			s.log.Warn("couldn't keep a match's last-hit curve", "match", m.MatchID, "err", err)
			continue
		}
		filled++
	}
	if filled > 0 {
		s.log.Info("added last-hit curves to imported matches", "matches", filled)
	}
}
