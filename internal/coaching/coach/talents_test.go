package coach

import (
	"testing"

	"gourdian/internal/game/dota"
	"gourdian/internal/game/gsi"
)

// talentSpec is a rule over the talent values, the way a player's rule would use them.
var talentSpec = RuleSpec{ID: "custom-talent", Name: "Talent", Enabled: true, Category: "skills", Match: "all",
	When:     Trigger{Type: WhenState, For: 20},
	If:       []Cond{{Field: "talent_points", Op: "ge", Num: 1}},
	Then:     AlertSpec{Text: "Your level {talent_level} talent is unspent", Severity: "warn"},
	Cooldown: 60}

// atLevel is a hero standing at a level with talents taken, and nothing else going on.
func atLevel(level, taken int) func(*gsi.State) {
	return func(s *gsi.State) {
		s.Hero.Level = level
		for i, on := range []*bool{&s.Hero.Talent1, &s.Hero.Talent2, &s.Hero.Talent3, &s.Hero.Talent4,
			&s.Hero.Talent5, &s.Hero.Talent6, &s.Hero.Talent7, &s.Hero.Talent8} {
			*on = i < taken
		}
	}
}

func talentTips(t *testing.T, level, taken, from, to int) []Tip {
	t.Helper()
	return byRule(play(customEngine(t, talentSpec), settings(dota.Carry), from, to, atLevel(level, taken)), "custom-talent")
}

// A talent point the player never spent is there to be seen, level by level.
func TestTalentValuesCountWhatIsWaiting(t *testing.T) {
	got := talentTips(t, 10, 0, 600, 700)
	if len(got) != 2 || got[0].Clock != 620 || got[1].Clock != 680 {
		t.Fatalf("want an alert at 620 and 680, got %+v", got)
	}
	if got[0].Text != "Your level 10 talent is unspent" {
		t.Errorf("text = %q", got[0].Text)
	}
}

// The level named is the talent actually waiting, not the first one the hero ever got.
func TestTalentLevelIsTheOneWaiting(t *testing.T) {
	got := talentTips(t, 20, 2, 1200, 1230)
	if len(got) != 1 || got[0].Text != "Your level 20 talent is unspent" {
		t.Fatalf("want the level 20 talent named once, got %+v", got)
	}
}

func TestNoTalentPointsWhenThereAreNone(t *testing.T) {
	for _, c := range []struct {
		name         string
		level, taken int
	}{
		{"below level 10", 9, 0},
		{"talent taken", 10, 1},
		{"every talent taken", 25, 4},
	} {
		if got := talentTips(t, c.level, c.taken, 600, 700); len(got) > 0 {
			t.Errorf("%s: %+v", c.name, got)
		}
	}
}

// Talents have their own points since 7.40, so an unspent one isn't an unspent skill point.
func TestTalentIsNotCountedAsASkillPoint(t *testing.T) {
	tips := play(newEngine(nil), settings(dota.Carry), 600, 700, func(s *gsi.State) {
		atLevel(10, 0)(s)
		// Ten levels, ten ability points, all spent.
		s.Abilities = map[string]gsi.Ability{"ability0": {Name: "antimage_mana_break", Level: 4},
			"ability1": {Name: "antimage_blink", Level: 4}, "ability2": {Name: "antimage_counterspell", Level: 2}}
	})
	if got := byRule(tips, "skill_points"); len(got) > 0 {
		t.Errorf("an unspent talent was reported as a skill point: %+v", got)
	}
}
