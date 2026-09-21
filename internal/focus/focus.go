// Package focus chooses the one thing to work on in the next game, out of what past reviews
// asked for: this hero and position first, then this position on any hero, then any game at all.
package focus

import (
	"strings"

	"gourdian/internal/dota"
	"gourdian/internal/i18n"
	"gourdian/internal/model"
)

// Pick is the focus for a match about to start, worded in lang, or "" when no review set one.
// A focus borrowed from another position says where it came from.
func Pick(reviews []model.Review, role string, heroID int, lang string) string {
	var sameHero, sameRole, any model.Review
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
		return sameHero.NextGameFocus
	case sameRole.NextGameFocus != "":
		return sameRole.NextGameFocus
	case any.NextGameFocus != "":
		return fromRole(any, lang)
	}
	return ""
}

// Last is the focus the previous review set for this position, so the next review can check
// whether it happened.
func Last(reviews []model.Review, role string, heroID int) string {
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
