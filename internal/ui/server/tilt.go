package server

import (
	"time"

	"gourdian/internal/coaching/coach"
	"gourdian/internal/coaching/tilt"
	"gourdian/internal/data/stats"
	"gourdian/internal/game/model"
	"gourdian/internal/i18n"
	"gourdian/internal/sys/config"
)

// tiltCheck warns after a match that ends a losing run.
func (s *Server) tiltCheck(m model.MatchSummary, set config.Settings) {
	if !set.TiltCheck || !m.Real() {
		return
	}
	// Turbo counts here, unlike in averages and targets: a run of losses is a run of losses.
	matches, err := s.stats.MatchesWhere(stats.MatchFilter{Since: time.Now().Add(-24 * time.Hour), Real: true, Turbo: true})
	if err != nil {
		return
	}
	mmr, _ := s.stats.MMR()
	reason := tilt.Reason(matches, mmr, set.Language)
	if reason == "" {
		return
	}
	s.brief.mu.Lock()
	s.brief.tiltAt = time.Now()
	s.brief.mu.Unlock()
	tip := coach.Tip{Rule: "tilt", Category: "focus", Severity: coach.Warn, Text: reason, Speech: reason + ".", Clock: m.DurationSec, At: time.Now()}
	if set.Language != "en" {
		tip.SpeechEN = tilt.Reason(matches, mmr, "en") + "."
	}
	s.emitTips(m.MatchID, []coach.Tip{tip}, set)
}

// tiltReminder gives one calm-down reminder when a match starts soon after a break warning.
func (s *Server) tiltReminder(matchID string, set config.Settings) {
	s.brief.mu.Lock()
	recent := !s.brief.tiltAt.IsZero() && time.Since(s.brief.tiltAt) < tilt.RemindFor
	s.brief.tiltAt = time.Time{}
	s.brief.mu.Unlock()
	if !recent || !set.TiltCheck {
		return
	}
	tip := coach.Tip{Rule: "tilt", Category: "focus", Severity: coach.Info, At: time.Now(),
		Text:   i18n.Say(set.Language, "Straight back in after a losing run: play this one calm, mute anyone tilting you, and focus on your own farm"),
		Speech: i18n.Say(set.Language, "Play this one calm. Mute anyone tilting you.")}
	if set.Language != "en" {
		tip.SpeechEN = i18n.Say("en", "Play this one calm. Mute anyone tilting you.")
	}
	s.emitTips(matchID, []coach.Tip{tip}, set)
}
