// Package focus chooses the one thing to work on next, out of what past reviews asked for.
package focus

import (
	"cmp"
	"strings"

	"gourdian/internal/game/dota"
	"gourdian/internal/game/model"
	"gourdian/internal/i18n"
)

// Pick prefers this hero and position, then the position, then any game, worded in lang.
func Pick(reviews []model.Review, role string, heroID int, lang string) string {
	var sameHero, sameRole string
	var any model.Review
	for _, r := range reviews {
		if strings.TrimSpace(r.NextGameFocus) == "" {
			continue
		}
		any = r
		if r.Role == role || r.Role == "" {
			sameRole = r.NextGameFocus
		}
		if r.Role == role && heroID > 0 && r.HeroID == heroID {
			sameHero = r.NextGameFocus
		}
	}
	return cmp.Or(sameHero, sameRole, fromRole(any, lang))
}

// Last is what the previous review asked of this position, so the next one can check it happened.
func Last(reviews []model.Review, role string, heroID int) string {
	best := ""
	for _, r := range reviews {
		if strings.TrimSpace(r.NextGameFocus) == "" {
			continue
		}
		// The hero only has to match for a review saved before positions were recorded.
		if r.Role == role || (r.Role == "" && heroID == r.HeroID) {
			best = r.NextGameFocus
		}
	}
	return best
}

func fromRole(r model.Review, lang string) string {
	name := dota.RoleName(r.Role, lang)
	switch {
	case name != "" && r.Hero != "":
		return i18n.Say(lang, "%s (from your %s game as %s)", r.NextGameFocus, r.Hero, name)
	case r.Hero != "":
		return i18n.Say(lang, "%s (from your %s game)", r.NextGameFocus, r.Hero)
	}
	return r.NextGameFocus
}
