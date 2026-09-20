package server

import (
	"slices"
	"time"

	"gourdian/internal/coach"
	"gourdian/internal/config"
	"gourdian/internal/model"
	"gourdian/internal/stats"
)

const (
	sessionGap    = 90 * time.Minute // matches closer than this belong to one session
	tiltRemindFor = 10 * time.Minute // a match started this soon after the warning gets a reminder
	tiltMMRDrop   = 50
)

// tiltReason looks at the session the latest match ended and says why a break would help,
// or returns "".
func tiltReason(matches []model.MatchSummary, mmr []model.MMREntry, lang string) string {
	var decided []model.MatchSummary
	for _, m := range matches {
		if m.Real() && m.Result != "unknown" {
			decided = append(decided, m)
		}
	}
	slices.SortFunc(decided, func(a, b model.MatchSummary) int { return a.EndedAt.Compare(b.EndedAt) })
	n := len(decided)
	if n == 0 {
		return ""
	}
	start := n - 1
	for start > 0 && decided[start].EndedAt.Sub(decided[start-1].EndedAt) <= sessionGap {
		start--
	}
	session := decided[start:]
	lost := func(i int) bool { return session[i].Result == "loss" }
	k := len(session)
	switch {
	case k >= 3 && lost(k-1) && lost(k-2) && lost(k-3):
		return roleSay(lang, "Three losses in a row. Take a proper break before you queue again")
	case k >= 2 && lost(k-1) && lost(k-2):
		return roleSay(lang, "Two losses in a row. Take a 10-minute break before you queue again")
	case k >= 4 && lost(k-1):
		losses := 0
		for i := k - 4; i < k; i++ {
			if lost(i) {
				losses++
			}
		}
		if losses >= 3 {
			return roleSay(lang, "Three of your last four games were losses. Take a break before the next one")
		}
	}
	var first, last *model.MMREntry
	for i := range mmr {
		if e := &mmr[i]; !e.Date.Before(session[0].EndedAt.Add(-3 * time.Hour)) {
			if first == nil {
				first = e
			}
			last = e
		}
	}
	if first != nil && last != first && first.MMR-last.MMR >= tiltMMRDrop && lost(k-1) {
		return roleSay(lang, "You're down %d MMR this session. Take a break before you queue again", first.MMR-last.MMR)
	}
	return ""
}

// tiltCheck warns after a match that ends a losing run.
func (s *Server) tiltCheck(m model.MatchSummary, set config.Settings) {
	if !set.TiltCheck || !m.Real() {
		return
	}
	// tiltReason looks at the current session of games, which fits in a day.
	// Turbo counts here. It is left out of averages and targets because it pays differently,
	// but a run of losses is a run of losses whatever the mode, and that is what this is for.
	matches, err := s.stats.MatchesWhere(stats.MatchFilter{Since: time.Now().Add(-24 * time.Hour), Real: true, Turbo: true})
	if err != nil {
		return
	}
	mmr, _ := s.stats.MMR()
	reason := tiltReason(matches, mmr, set.Language)
	if reason == "" {
		return
	}
	s.brief.mu.Lock()
	s.brief.tiltAt = time.Now()
	s.brief.mu.Unlock()
	tip := coach.Tip{Rule: "tilt", Category: "focus", Severity: coach.Warn, Text: reason, Speech: reason + ".", Clock: m.DurationSec, At: time.Now()}
	if set.Language != "en" {
		tip.SpeechEN = tiltReason(matches, mmr, "en") + "."
	}
	s.emitTips(m.MatchID, []coach.Tip{tip}, set)
}

// tiltReminder gives one calm-down reminder when a match starts soon after a break warning.
func (s *Server) tiltReminder(matchID string, set config.Settings) {
	s.brief.mu.Lock()
	recent := !s.brief.tiltAt.IsZero() && time.Since(s.brief.tiltAt) < tiltRemindFor
	s.brief.tiltAt = time.Time{}
	s.brief.mu.Unlock()
	if !recent || !set.TiltCheck {
		return
	}
	tip := coach.Tip{Rule: "tilt", Category: "focus", Severity: coach.Info, At: time.Now(),
		Text:   roleSay(set.Language, "Straight back in after a losing run: play this one calm, mute anyone tilting you, and focus on your own farm"),
		Speech: roleSay(set.Language, "Play this one calm. Mute anyone tilting you.")}
	if set.Language != "en" {
		tip.SpeechEN = roleSay("en", "Play this one calm. Mute anyone tilting you.")
	}
	s.emitTips(matchID, []coach.Tip{tip}, set)
}
