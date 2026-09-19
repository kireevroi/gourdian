package coach

import (
	"testing"

	"gourdian/internal/config"
	"gourdian/internal/gsi"
)

// A rule that fires every few seconds, to see what a fight does to it.
var nagSpec = RuleSpec{ID: "custom-nag", Name: "Nag", Enabled: true, Category: "economy", Match: "all",
	When: Trigger{Type: WhenState}, If: []Cond{{Field: "gold", Op: "ge", Num: 100}},
	Then: AlertSpec{Text: "Spend it", Severity: "info"}, Cooldown: 5}

func TestFightsHoldBackSpokenReminders(t *testing.T) {
	set := settings(config.RoleHardSupport)
	set.QuietInFights = true
	hurt := func(s *gsi.State) {
		s.Player.Gold = 900
		s.Hero.HealthPercent = 90
		if s.Map.ClockTime >= 620 {
			s.Hero.HealthPercent = 50 // a fight starts
		}
	}
	e := customEngine(t)
	e.SetCustomRules([]RuleSpec{nagSpec})
	tips := byRule(play(e, set, 600, 640, hurt), "custom-nag")
	var inFight, after int
	for _, tip := range tips {
		switch {
		case tip.Clock >= 620 && tip.Clock <= 626:
			inFight++
			if !tip.Quiet {
				t.Fatalf("an ordinary reminder was spoken mid-fight: %+v", tip)
			}
		case tip.Clock > 626 && !tip.Quiet:
			after++
		}
	}
	if inFight == 0 || after == 0 {
		t.Fatalf("want quiet during the fight and speech after it: %d in, %d after", inFight, after)
	}

	set.QuietInFights = false
	e2 := customEngine(t)
	e2.SetCustomRules([]RuleSpec{nagSpec})
	for _, tip := range byRule(play(e2, set, 600, 640, hurt), "custom-nag") {
		if tip.Quiet {
			t.Fatalf("with the setting off nothing should be held back: %+v", tip)
		}
	}
}
