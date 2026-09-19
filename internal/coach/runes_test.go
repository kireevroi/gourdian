package coach

import (
	"strings"
	"testing"

	"dotatrainer/internal/config"
	"dotatrainer/internal/gsi"
)

func TestPowerRunesOnScheduleOnlyWhileLaning(t *testing.T) {
	var at []int
	for _, tip := range byRule(play(newEngine(nil), settings(config.RoleMid), 300, 1500, nil), "power_runes") {
		at = append(at, tip.Clock)
	}
	if len(at) != 2 || at[0] != 345 || at[1] != 465 {
		t.Fatalf("a mid should hear the 6:00 and 8:00 power runes and no later ones, got %v", at)
	}
}

func TestPowerRunesAfterLaningOnlyWhenNearOne(t *testing.T) {
	far := byRule(play(newEngine(nil), settings(config.RoleCarry), 700, 900, nil), "power_runes_near")
	if len(far) != 0 {
		t.Fatalf("far from both rune spots: %+v", far)
	}
	nearTop := func(s *gsi.State) { s.Hero.XPos, s.Hero.YPos = -2600, 1800 }
	got := byRule(play(newEngine(nil), settings(config.RoleCarry), 700, 900, nearTop), "power_runes_near")
	if len(got) != 2 || got[0].Clock != 705 || got[1].Clock != 825 || !strings.Contains(got[0].Text, "top") {
		t.Fatalf("near the top rune from 11:40 to 15:00: want reminders at 11:45 and 13:45, got %+v", got)
	}
}
