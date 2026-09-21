package coach

import (
	"fmt"
	"strings"
	"testing"

	"gourdian/internal/game/dota"
	"gourdian/internal/game/gsi"
)

// withAegis plays a match where player holder picks up the Aegis at 16:20 (Roshan died at 16:18).
// The player is radiant's third slot, so player 2.
func withAegis(holder int, change func(s *gsi.State)) func(*gsi.State) {
	slot := 2
	return func(s *gsi.State) {
		s.Player.TeamSlot = &slot
		if s.Map.ClockTime >= 980 {
			s.Events = []gsi.Event{
				{GameTime: 978 + 90, EventType: "roshan_killed", KilledByTeam: "radiant"},
				{GameTime: 980 + 90, EventType: "aegis_picked_up", PlayerID: holder},
			}
			if holder == slot && s.Map.ClockTime < 1034 {
				s.Items["slot2"] = gsi.Item{Name: "item_aegis"}
			}
		}
		if change != nil {
			change(s)
		}
	}
}

func TestUsedAegisIsNeitherADeathNorAnExpiry(t *testing.T) {
	reincarnate := func(s *gsi.State) {
		if c := s.Map.ClockTime; c >= 1034 && c < 1039 {
			s.Hero.Alive, s.Hero.Health, s.Hero.HealthPercent, s.Hero.RespawnSeconds = false, 0, 0, 1039-c
		}
		if c := s.Map.ClockTime; c >= 1100 && c < 1130 {
			s.Hero.Alive, s.Hero.RespawnSeconds = false, 1130-c
		}
	}
	tips := play(newEngine(nil), settings(dota.Carry), 970, 1300, withAegis(2, reincarnate))
	if got := byRule(tips, "aegis"); len(got) != 0 {
		t.Fatalf("the Aegis was used at 17:14, so it can't expire at 21:20: %+v", got)
	}
	deaths := byRule(tips, "death")
	if len(deaths) != 1 || deaths[0].Clock != 1100 || !strings.Contains(deaths[0].Text, "death #1") {
		t.Fatalf("coming back with the Aegis isn't a death; the real one at 18:20 is the first: %+v", deaths)
	}
}

// The trainer speaks about the Aegis it can watch and no other. Its own leaves the player's
// inventory where it is seen to go; Dota never says when anyone else's is spent, so a
// team-mate who quietly survives their five minutes looks exactly like one who was brought
// back. A coach once told this player the team's Aegis had 48 seconds while it was spent.
func TestAegisAlertIsOnlyForYourOwn(t *testing.T) {
	for _, holder := range []struct {
		id   int
		name string
	}{{4, "a team-mate's"}, {7, "the enemy's"}} {
		tips := play(newEngine(nil), settings(dota.Carry), 970, 1260, withAegis(holder.id, nil))
		if got := byRule(tips, "aegis"); len(got) != 0 {
			t.Errorf("%s Aegis is a guess and should go unmentioned: %+v", holder.name, got)
		}
	}
	tips := play(newEngine(nil), settings(dota.Carry), 970, 1260, withAegis(2, func(s *gsi.State) {
		if s.Map.ClockTime >= 980 {
			s.Items["slot2"] = gsi.Item{Name: "item_aegis"}
		}
	}))
	if got := byRule(tips, "aegis"); len(got) != 1 || got[0].Text != "Your Aegis expires at 21:20" {
		t.Fatalf("the player's own Aegis: %+v", got)
	}
}

// The countdown the HUD and the AI coach read comes from the same place, so it appears for the
// player's own Aegis and for nobody else's.
func TestOnlyYourOwnAegisGetsATimer(t *testing.T) {
	timerAt := func(holder int, carried bool) bool {
		e := newEngine(nil)
		play(e, settings(dota.Carry), 970, 1100, withAegis(holder, func(s *gsi.State) {
			if carried && s.Map.ClockTime >= 980 {
				s.Items["slot2"] = gsi.Item{Name: "item_aegis"}
			}
		}))
		for _, timer := range e.Snapshot(settings(dota.Carry)).Timers {
			if strings.Contains(timer.Label, "Aegis") {
				return true
			}
		}
		return false
	}
	if timerAt(4, false) {
		t.Error("a team-mate's Aegis should have no timer")
	}
	if !timerAt(2, true) {
		t.Error("the player's own Aegis should have one")
	}
}

func TestRoshanKillerInRussian(t *testing.T) {
	e := newEngine(nil)
	e.SetLanguage("ru")
	set := settings(dota.Carry)
	set.Language = "ru"
	got := byRule(play(e, set, 970, 990, withAegis(4, nil)), "roshan")
	if len(got) != 1 || !strings.Contains(got[0].Text, "Рошан убит вашей командой") {
		t.Fatalf("russian roshan tip = %+v", got)
	}
}

// chat is a generic_event as GSI sends it at that clock.
func chat(clock int, data string) gsi.Event {
	return gsi.Event{GameTime: clock + 90, EventType: "generic_event", Data: data}
}

func TestAKilledHolderHasUsedTheirAegis(t *testing.T) {
	// The player is player 2 and carries the Aegis from 16:20; a chat line naming them as the
	// hero killed says it brought them back, whatever their inventory was seen to do.
	carrying := func(victim int) func(*gsi.State) {
		return func(s *gsi.State) {
			if s.Map.ClockTime >= 980 {
				s.Items["slot2"] = gsi.Item{Name: "item_aegis"}
			}
			if s.Map.ClockTime >= 1140 {
				kill := chat(1140, fmt.Sprintf(`{"type":"CHAT_MESSAGE_HERO_KILL","playerid1":%d,"playerid2":7,"time":1140.3}`, victim))
				s.Events = append([]gsi.Event{kill}, s.Events...)
			}
		}
	}
	if got := byRule(play(newEngine(nil), settings(dota.Carry), 970, 1260, withAegis(2, carrying(2))), "aegis"); len(got) != 0 {
		t.Fatalf("the player died at 19:00, so their Aegis was spent: %+v", got)
	}
	// Starting mid-match, the kill comes in the same update as the pickup, listed first.
	if got := byRule(play(newEngine(nil), settings(dota.Carry), 1140, 1260, withAegis(2, carrying(2))), "aegis"); len(got) != 0 {
		t.Fatalf("a kill listed before the pickup still comes after it: %+v", got)
	}
	if got := byRule(play(newEngine(nil), settings(dota.Carry), 970, 1260, withAegis(2, carrying(3))), "aegis"); len(got) != 1 {
		t.Fatalf("another hero's death leaves the player's Aegis alone: %+v", got)
	}
}
