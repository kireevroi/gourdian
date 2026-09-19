package coach

import (
	"testing"

	"dotatrainer/internal/config"
	"dotatrainer/internal/gsi"
)

func TestTowerDefenceAndWards(t *testing.T) {
	tips := play(newEngine(nil), settings(config.RoleHardSupport), 600, 660, func(s *gsi.State) {
		hp := 1800
		if s.Map.ClockTime >= 610 {
			hp = 900
		}
		if s.Map.ClockTime >= 620 {
			hp = 400
		}
		s.Buildings = map[string]map[string]gsi.Building{"radiant": {
			"dota_goodguys_tower1_mid": {Health: hp, MaxHealth: 1800},
			"dota_goodguys_tower1_top": {Health: 1800, MaxHealth: 1800},
		}}
		s.Map.WardPurchaseCooldown = 0
		if s.Map.ClockTime < 630 {
			s.Map.WardPurchaseCooldown = 40
		}
	})
	tower := byRule(tips, "tower_defence")
	if len(tower) != 1 || tower[0].Clock != 610 || tower[0].Text != "Your mid tier 1 tower is under attack (50% left). Defend it or TP in" {
		t.Fatalf("tower tips = %+v", tower)
	}
	// The stock filled at 630 and nobody bought a ward for 20 seconds.
	wards := byRule(tips, "wards_in_shop")
	if len(wards) != 1 || wards[0].Clock != 650 {
		t.Fatalf("ward tips = %+v", wards)
	}
}
