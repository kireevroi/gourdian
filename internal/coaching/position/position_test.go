package position

import (
	"strings"
	"testing"

	"gourdian/internal/game/dota"
)

func TestFromHeroRolesTakesTheFirstOneListed(t *testing.T) {
	cases := map[string][]string{
		dota.SoftSupport: {"Support", "Disabler", "Nuker", "Initiator"},
		dota.Carry:       {"Carry", "Pusher", "Escape"},
		"":               {"Initiator", "Durable"},
	}
	for want, roles := range cases {
		if got := FromHeroRoles(roles); got != want {
			t.Errorf("FromHeroRoles(%v) = %q, want %q", roles, got, want)
		}
	}
	if got := FromHeroRoles([]string{"Carry", "Support"}); got != dota.Carry {
		t.Errorf("the first listed role wins, got %q", got)
	}
	if got := FromHeroRoles(nil); got != "" {
		t.Errorf("FromHeroRoles(nil) = %q, want nothing", got)
	}
}

func TestWhatYouPlayedLastBeatsEverything(t *testing.T) {
	c := Candidates{Remembered: dota.Mid, Usual: dota.Carry, HeroRoles: []string{"Support"}}
	role, why := Choose(c, "Puck", "en")
	if role != dota.Mid {
		t.Fatalf("role = %q, want what was played last", role)
	}
	if !strings.Contains(why, "Puck") || !strings.Contains(why, "last time") {
		t.Fatalf("why = %q, want it to name the hero and the reason", why)
	}
}

func TestYourUsualComesNext(t *testing.T) {
	c := Candidates{Usual: dota.Carry, HeroRoles: []string{"Support"}}
	role, why := Choose(c, "Anti-Mage", "en")
	if role != dota.Carry || !strings.Contains(why, "usual") {
		t.Fatalf("role = %q, why = %q, want the usual position", role, why)
	}
}

func TestOpenDotaIsTheLastResort(t *testing.T) {
	c := Candidates{HeroRoles: []string{"Support", "Disabler"}}
	role, why := Choose(c, "Lion", "en")
	if role != dota.SoftSupport || !strings.Contains(why, "guess") {
		t.Fatalf("role = %q, why = %q, want a guess from the hero's roles", role, why)
	}
}

func TestAnUnknownHeroChoosesNothing(t *testing.T) {
	role, why := Choose(Candidates{}, "hero 999", "en")
	if role != "" || why != "" {
		t.Fatalf("role = %q, why = %q, want nothing rather than a default", role, why)
	}
	if role, _ := Choose(Candidates{HeroRoles: []string{"Durable"}}, "Pudge", "en"); role != "" {
		t.Fatalf("role = %q, want nothing when the hero's roles say neither", role)
	}
}

func TestTheReasonIsWordedInThePlayersLanguage(t *testing.T) {
	c := Candidates{Remembered: dota.Mid}
	_, en := Choose(c, "Puck", "en")
	_, ru := Choose(c, "Puck", "ru")
	if ru == "" || ru == en {
		t.Fatalf("Russian should not read as English: %q", ru)
	}
	if !strings.Contains(ru, "Puck") {
		t.Fatalf("the hero's name should survive translation: %q", ru)
	}
}
