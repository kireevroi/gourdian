package coach

import (
	"strings"
	"testing"

	"gourdian/internal/dota"
	"gourdian/internal/gsi"
)

// Top tier 1 is already low from earlier; the mid tier 1 at full health is now being hit.
func twoTowers(s *gsi.State) {
	mid := 1800
	if c := s.Map.ClockTime; c >= 700 {
		mid = max(1260, 1800-(c-700)*80) // never below 70%
	}
	s.Buildings = map[string]map[string]gsi.Building{"radiant": {
		"dota_goodguys_tower1_top": {Health: 400, MaxHealth: 1800}, // 22%, not under attack
		"dota_goodguys_tower1_mid": {Health: mid, MaxHealth: 1800},
	}}
}

func TestTowerAlertsTalkAboutTheTowerUnderAttack(t *testing.T) {
	tips := play(newEngine(nil), settings(dota.Mid), 690, 715, twoTowers)
	for _, tip := range byRule(tips, "tower_defence") {
		if strings.Contains(tip.Text, "22%") {
			t.Errorf("reported the top tower's health for the mid tower: %q", tip.Text)
		}
	}
	for _, tip := range byRule(tips, "glyph") {
		t.Errorf("a healthy tower shouldn't need a Glyph because another one is low: %q", tip.Text)
	}
}

func TestDiedHoldingGoldCountsTheGoldBeforeDying(t *testing.T) {
	tips := play(newEngine(nil), settings(dota.Carry), 600, 620, func(s *gsi.State) {
		s.Player.Gold, s.Player.GoldReliable = 1500, 100 // 1400 unreliable while alive
		if s.Map.ClockTime >= 610 {
			s.Hero.Alive, s.Hero.RespawnSeconds = false, 30
			s.Player.Gold = 800 // Dota takes part of the unreliable gold on death
		}
	})
	got := byRule(tips, "death_gold")
	if len(got) != 1 || !strings.Contains(got[0].Text, "1400") {
		t.Fatalf("want the 1400 unreliable gold held before dying, got %+v", got)
	}
	// Dota can take the gold an update before it says the hero died, showing 0 health first.
	tips = play(newEngine(nil), settings(dota.Carry), 600, 620, func(s *gsi.State) {
		s.Player.Gold, s.Player.GoldReliable = 1500, 100
		if c := s.Map.ClockTime; c >= 610 {
			s.Hero.Health, s.Player.Gold = 0, 800
			s.Hero.Alive = c == 610
		}
	})
	if got := byRule(tips, "death_gold"); len(got) != 1 || !strings.Contains(got[0].Text, "1400") {
		t.Fatalf("the gold was taken at 0 health, before the death: want 1400, got %+v", got)
	}
}
