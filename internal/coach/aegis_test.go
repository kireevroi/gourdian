package coach

import (
	"fmt"
	"strings"
	"testing"

	"gourdian/internal/config"
	"gourdian/internal/gsi"
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
	tips := play(newEngine(nil), settings(config.RoleCarry), 970, 1300, withAegis(2, reincarnate))
	if got := byRule(tips, "aegis"); len(got) != 0 {
		t.Fatalf("the Aegis was used at 17:14, so it can't expire at 21:20: %+v", got)
	}
	deaths := byRule(tips, "death")
	if len(deaths) != 1 || deaths[0].Clock != 1100 || !strings.Contains(deaths[0].Text, "death #1") {
		t.Fatalf("coming back with the Aegis isn't a death; the real one at 18:20 is the first: %+v", deaths)
	}
}

func TestAegisAlertSaysWhoseItIs(t *testing.T) {
	tips := play(newEngine(nil), settings(config.RoleCarry), 970, 1260, withAegis(4, nil))
	got := byRule(tips, "aegis")
	if len(got) != 1 || !strings.HasPrefix(got[0].Text, "Your team's Aegis expires at 21:20") || !strings.Contains(got[0].Text, "if it wasn't used") {
		t.Fatalf("a teammate's Aegis: %+v", got)
	}
	tips = play(newEngine(nil), settings(config.RoleCarry), 970, 1260, withAegis(7, nil))
	if got := byRule(tips, "aegis"); len(got) != 1 || !strings.HasPrefix(got[0].Text, "The enemy's Aegis") {
		t.Fatalf("the enemy's Aegis: %+v", got)
	}
	tips = play(newEngine(nil), settings(config.RoleCarry), 970, 1260, withAegis(2, func(s *gsi.State) {
		if s.Map.ClockTime >= 980 {
			s.Items["slot2"] = gsi.Item{Name: "item_aegis"}
		}
	}))
	if got := byRule(tips, "aegis"); len(got) != 1 || got[0].Text != "Your Aegis expires at 21:20" {
		t.Fatalf("the player's own Aegis: %+v", got)
	}
}

func TestRoshanKillerInRussian(t *testing.T) {
	e := newEngine(nil)
	e.SetLanguage("ru")
	set := settings(config.RoleCarry)
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
	killed := func(victim int) func(*gsi.State) {
		return func(s *gsi.State) {
			if s.Map.ClockTime >= 1140 {
				kill := chat(1140, fmt.Sprintf(`{"type":"CHAT_MESSAGE_HERO_KILL","playerid1":%d,"playerid2":7,"time":1140.3}`, victim))
				s.Events = append([]gsi.Event{kill}, s.Events...)
			}
		}
	}
	if got := byRule(play(newEngine(nil), settings(config.RoleCarry), 970, 1260, withAegis(4, killed(4))), "aegis"); len(got) != 0 {
		t.Fatalf("the teammate holding the Aegis died at 19:00, so it was already used: %+v", got)
	}
	// Starting mid-match, the kill comes in the same update as the pickup, listed first.
	if got := byRule(play(newEngine(nil), settings(config.RoleCarry), 1140, 1260, withAegis(4, killed(4))), "aegis"); len(got) != 0 {
		t.Fatalf("a kill listed before the pickup still comes after it: %+v", got)
	}
	if got := byRule(play(newEngine(nil), settings(config.RoleCarry), 970, 1260, withAegis(4, killed(3))), "aegis"); len(got) != 1 {
		t.Fatalf("another hero's death leaves the Aegis alone: %+v", got)
	}
}
