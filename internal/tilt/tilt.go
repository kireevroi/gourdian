// Package tilt spots the losing run that a break would help with, within one sitting.
package tilt

import (
	"slices"
	"time"

	"gourdian/internal/i18n"
	"gourdian/internal/model"
)

const (
	sessionGap = 90 * time.Minute
	mmrDrop    = 50

	// RemindFor is how soon after a warning a new match still earns a calm-down reminder.
	RemindFor = 10 * time.Minute
)

// Reason says why a break would help after the latest match, worded in lang, or "".
func Reason(matches []model.MatchSummary, mmr []model.MMREntry, lang string) string {
	session := lastSession(matches)
	k := len(session)
	if k == 0 {
		return ""
	}
	lost := func(i int) bool { return session[i].Result == "loss" }
	switch {
	case k >= 3 && lost(k-1) && lost(k-2) && lost(k-3):
		return i18n.Say(lang, "Three losses in a row. Take a proper break before you queue again")
	case k >= 2 && lost(k-1) && lost(k-2):
		return i18n.Say(lang, "Two losses in a row. Take a 10-minute break before you queue again")
	case k >= 4 && lost(k-1):
		losses := 0
		for i := k - 4; i < k; i++ {
			if lost(i) {
				losses++
			}
		}
		if losses >= 3 {
			return i18n.Say(lang, "Three of your last four games were losses. Take a break before the next one")
		}
	}
	if drop, ok := sessionDrop(mmr, session[0].EndedAt); ok && drop >= mmrDrop && lost(k-1) {
		return i18n.Say(lang, "You're down %d MMR this session. Take a break before you queue again", drop)
	}
	return ""
}

// lastSession is the run of decided real matches the latest one belongs to, oldest first.
func lastSession(matches []model.MatchSummary) []model.MatchSummary {
	var decided []model.MatchSummary
	for _, m := range matches {
		if m.Real() && m.Result != "unknown" {
			decided = append(decided, m)
		}
	}
	slices.SortFunc(decided, func(a, b model.MatchSummary) int { return a.EndedAt.Compare(b.EndedAt) })
	n := len(decided)
	if n == 0 {
		return nil
	}
	start := n - 1
	for start > 0 && decided[start].EndedAt.Sub(decided[start-1].EndedAt) <= sessionGap {
		start--
	}
	return decided[start:]
}

func sessionDrop(mmr []model.MMREntry, start time.Time) (int, bool) {
	var first, last *model.MMREntry
	for i := range mmr {
		// Entries are logged after a session starts, so reach back for the one it began on.
		if e := &mmr[i]; !e.Date.Before(start.Add(-3 * time.Hour)) {
			if first == nil {
				first = e
			}
			last = e
		}
	}
	if first == nil || last == first {
		return 0, false
	}
	return first.MMR - last.MMR, true
}
