package coach

import (
	"fmt"
	"strings"
	"testing"

	"gourdian/internal/dota"
	"gourdian/internal/gsi"
)

// push drops the mid tier 1 tower from full to 30% over 20 seconds, starting at 10:00.
func push(s *gsi.State) {
	hp := 1800
	if c := s.Map.ClockTime; c >= 600 {
		hp = max(540, 1800-(c-600)*63)
	}
	s.Buildings = map[string]map[string]gsi.Building{"radiant": {
		"dota_goodguys_tower1_mid": {Health: hp, MaxHealth: 1800},
		"dota_goodguys_tower2_mid": {Health: 2500, MaxHealth: 2500},
		"good_rax_melee_mid":       {Health: 2200, MaxHealth: 2200},
		"dota_goodguys_tower3_mid": {Health: 2500, MaxHealth: 2500},
	}}
}

func TestGlyphWhenATowerIsPushedHard(t *testing.T) {
	got := byRule(play(newEngine(nil), settings(dota.Mid), 590, 640, push), "glyph")
	if len(got) != 1 || !strings.Contains(got[0].Text, "mid tier 1 tower is dropping fast") || got[0].Severity != Urgent {
		t.Fatalf("glyph tips = %+v", got)
	}
	// A creep wave chipping the tower is not a reason to Glyph.
	slow := func(s *gsi.State) {
		hp := 1800 - max(0, s.Map.ClockTime-600)*5
		s.Buildings = map[string]map[string]gsi.Building{"radiant": {"dota_goodguys_tower1_mid": {Health: hp, MaxHealth: 1800}}}
	}
	if got := byRule(play(newEngine(nil), settings(dota.Mid), 590, 700, slow), "glyph"); len(got) != 0 {
		t.Fatalf("a slow chip triggered the glyph: %+v", got)
	}
}

func TestGlyphInRussianNamesTheBuilding(t *testing.T) {
	e := newEngine(nil)
	e.SetLanguage("ru")
	set := settings(dota.Mid)
	set.Language = "ru"
	got := byRule(play(e, set, 590, 640, push), "glyph")
	if len(got) != 1 || !strings.Contains(got[0].Text, "башня 1-го тира на миде") {
		t.Fatalf("russian glyph tip = %+v", got)
	}
	// Without a Russian voice the English line is spoken, so it must name the tower in English.
	if got[0].SpeechEN != "Glyph your mid tier 1 tower" {
		t.Fatalf("english fallback = %q", got[0].SpeechEN)
	}
}

func TestGlyphRefreshedAndBuybackToDefend(t *testing.T) {
	raxDown := func(s *gsi.State) {
		push(s)
		if s.Map.ClockTime >= 1810 {
			s.Buildings["radiant"]["good_rax_melee_mid"] = gsi.Building{Health: 0, MaxHealth: 2200}
		}
	}
	if got := byRule(play(newEngine(nil), settings(dota.Carry), 1800, 1830, raxDown), "glyph_refreshed"); len(got) != 1 {
		t.Fatalf("glyph refresh tips = %+v", got)
	}
	dead := func(s *gsi.State) {
		s.Hero.Alive, s.Hero.RespawnSeconds = false, 45
		s.Hero.BuybackCost, s.Player.Gold = 1500, 3000
		hp := 2500 - max(0, s.Map.ClockTime-1800)*40
		s.Buildings = map[string]map[string]gsi.Building{"radiant": {"dota_goodguys_tower3_mid": {Health: hp, MaxHealth: 2500}}}
	}
	got := byRule(play(newEngine(nil), settings(dota.Carry), 1800, 1830, dead), "buyback_defend")
	if len(got) != 1 || !strings.Contains(got[0].Text, "mid tier 3 tower is under attack") {
		t.Fatalf("buyback tips = %+v", got)
	}
}

func TestNoGlyphTipWhileYourGlyphCoolsDown(t *testing.T) {
	// GSI lists the newest event first.
	pushWith := func(events ...gsi.Event) func(*gsi.State) {
		return func(s *gsi.State) {
			push(s)
			for _, ev := range events {
				if ev.GameTime <= s.Map.GameTime {
					s.Events = append([]gsi.Event{ev}, s.Events...)
				}
			}
		}
	}
	glyphs := func(events ...gsi.Event) []Tip {
		return byRule(play(newEngine(nil), settings(dota.Mid), 585, 640, pushWith(events...)), "glyph")
	}
	ours := chat(590, `{"type":"CHAT_MESSAGE_GLYPH_USED","playerid1":2,"time":590.4}`)
	if got := glyphs(ours); len(got) != 0 {
		t.Fatalf("your team used its Glyph at 9:50: %+v", got)
	}
	if got := glyphs(chat(590, `{"type":"CHAT_MESSAGE_GLYPH_USED","playerid1":3,"time":590.4}`)); len(got) != 1 {
		t.Fatalf("the enemy's Glyph isn't yours: %+v", got)
	}
	lost := `{"type":"CHAT_MESSAGE_TOWER_KILL","value":3,"playerid1":7,"value3":1,"time":%s}`
	if got := glyphs(ours, chat(595, fmt.Sprintf(lost, "595.5"))); len(got) != 1 {
		t.Fatalf("losing a tower brings the Glyph back: %+v", got)
	}
	if got := glyphs(chat(590, fmt.Sprintf(lost, "590.1")), ours); len(got) != 0 {
		t.Fatalf("a tower lost just before the Glyph doesn't bring it back: %+v", got)
	}
	taken := chat(595, `{"type":"CHAT_MESSAGE_TOWER_KILL","value":2,"playerid1":1,"value3":1,"time":595.5}`)
	if got := glyphs(ours, taken); len(got) != 0 {
		t.Fatalf("taking an enemy tower doesn't bring your Glyph back: %+v", got)
	}
}
