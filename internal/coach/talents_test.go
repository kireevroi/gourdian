package coach

import (
	"testing"

	"gourdian/internal/dota"
	"gourdian/internal/gsi"
)

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

// A talent costs nothing but a click, so the trainer says which level's talent is waiting.
func TestUnspentTalentIsNoticed(t *testing.T) {
	got := byRule(play(newEngine(nil), settings(dota.Carry), 600, 700, atLevel(10, 0)), "talent")
	if len(got) != 2 || got[0].Clock != 620 || got[1].Clock != 680 {
		t.Fatalf("want a talent alert at 620 and 680, got %+v", got)
	}
	if got[0].Text != "Your level 10 talent is unspent. Take one from the talent tree" {
		t.Errorf("text = %q", got[0].Text)
	}
	if got[0].Speech != "Take your level 10 talent" {
		t.Errorf("speech = %q", got[0].Speech)
	}
}

// The alert names the talent that is actually waiting, not the first one the hero ever got.
func TestTalentAlertNamesTheWaitingLevel(t *testing.T) {
	got := byRule(play(newEngine(nil), settings(dota.Mid), 1200, 1230, atLevel(20, 2)), "talent")
	if len(got) != 1 || got[0].Text != "Your level 20 talent is unspent. Take one from the talent tree" {
		t.Fatalf("want the level 20 talent named once, got %+v", got)
	}
}

func TestTalentsTakenSaysNothing(t *testing.T) {
	for _, c := range []struct {
		name         string
		level, taken int
	}{
		{"below level 10", 9, 0},
		{"talent taken", 10, 1},
		{"every talent taken", 25, 4},
	} {
		if got := byRule(play(newEngine(nil), settings(dota.Carry), 600, 700, atLevel(c.level, c.taken)), "talent"); len(got) > 0 {
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
	if got := byRule(tips, "talent"); len(got) == 0 {
		t.Error("the unspent talent went unnoticed")
	}
}
