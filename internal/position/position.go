// Package position works out which position to coach a hero as when the player hasn't said.
package position

import (
	"slices"

	"gourdian/internal/dota"
	"gourdian/internal/i18n"
)

// Candidates is what is known about a hero, most trusted first.
type Candidates struct {
	Remembered string
	Usual      string
	HeroRoles  []string
}

// Choose picks the position and says where it came from; both are empty when nothing is known.
func Choose(c Candidates, hero, lang string) (role, why string) {
	switch {
	case c.Remembered != "":
		return c.Remembered, i18n.Say(lang, "what you played on %s last time", hero)
	case c.Usual != "":
		return c.Usual, i18n.Say(lang, "your usual role on %s", hero)
	}
	if r := FromHeroRoles(c.HeroRoles); r != "" {
		return r, i18n.Say(lang, "a guess for %s", hero)
	}
	return "", ""
}

// FromHeroRoles reads OpenDota's hero roles, which list the main ones first.
func FromHeroRoles(roles []string) string {
	support, carry := slices.Index(roles, "Support"), slices.Index(roles, "Carry")
	switch {
	case support >= 0 && (carry < 0 || support < carry):
		return dota.SoftSupport
	case carry >= 0:
		return dota.Carry
	}
	return ""
}
