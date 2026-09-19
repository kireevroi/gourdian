package coach

import (
	"testing"

	"dotatrainer/internal/dotadata"
	"dotatrainer/internal/gsi"
)

func stormState(level int, levels map[string]int) *gsi.State {
	s := state(300)
	s.Hero.ID, s.Hero.Name = 17, "npc_dota_hero_storm_spirit"
	s.Hero.Level = level
	s.Abilities = map[string]gsi.Ability{}
	for i, name := range []string{"storm_spirit_static_remnant", "storm_spirit_electric_vortex", "storm_spirit_overload", "storm_spirit_ball_lightning"} {
		s.Abilities["ability"+string(rune('0'+i))] = gsi.Ability{Name: name, Level: levels[name], Ultimate: i == 3}
	}
	return s
}

var stormOrder = []string{
	"storm_spirit_static_remnant", "storm_spirit_overload", "storm_spirit_static_remnant", "storm_spirit_electric_vortex",
	"storm_spirit_static_remnant", "storm_spirit_ball_lightning", "storm_spirit_static_remnant",
}

func TestNextSkillFollowsThePlayersLevels(t *testing.T) {
	cases := []struct {
		name   string
		level  int
		levels map[string]int
		want   string
	}{
		{"first point", 1, nil, "storm_spirit_static_remnant"},
		{"on the order", 3, map[string]int{"storm_spirit_static_remnant": 1, "storm_spirit_overload": 1}, "storm_spirit_static_remnant"},
		{"skipped overload earlier", 3, map[string]int{"storm_spirit_static_remnant": 2}, "storm_spirit_overload"},
		{"ultimate at 6", 6, map[string]int{"storm_spirit_static_remnant": 3, "storm_spirit_overload": 1, "storm_spirit_electric_vortex": 1}, "storm_spirit_ball_lightning"},
		// Remnant 3 needs hero level 5; the next pick the hero can take is Vortex.
		{"too low for the next level", 4, map[string]int{"storm_spirit_static_remnant": 2, "storm_spirit_overload": 1}, "storm_spirit_electric_vortex"},
		{"order done", 7, map[string]int{"storm_spirit_static_remnant": 4, "storm_spirit_overload": 1, "storm_spirit_electric_vortex": 1, "storm_spirit_ball_lightning": 1}, ""},
	}
	for _, c := range cases {
		if got := nextSkill(stormOrder, stormState(c.level, c.levels)); got != c.want {
			t.Errorf("%s: next = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestSkillPointAlertNamesTheProSkill(t *testing.T) {
	levelUp := func(s *gsi.State) {
		storm := stormState(1, map[string]int{"storm_spirit_static_remnant": 1})
		s.Hero.ID, s.Hero.Name, s.Hero.Level, s.Abilities = 17, storm.Hero.Name, 1, storm.Abilities
		if s.Map.ClockTime >= 100 {
			s.Hero.Level = 2
		}
	}
	e := newEngine(fakeData{skills: &dotadata.SkillBuild{HeroID: 17, Position: 2, Games: 300, Order: stormOrder}})
	set := settings("mid")
	got := byRule(play(e, set, 60, 160, levelUp), "skill_points")
	if len(got) == 0 || got[0].Text != "You have an unspent skill point. Pros level storm_spirit_overload now (mid, 300 pro games)" ||
		got[0].Speech != "Pros level storm_spirit_overload now" {
		t.Fatalf("tips = %+v", got)
	}
	snap := e.Snapshot(set)
	if snap.Skill == nil || snap.Skill.Next != "storm_spirit_overload" || snap.Skill.Points != 1 || len(snap.Skill.Order) != len(stormOrder) {
		t.Fatalf("skill view = %+v", snap.Skill)
	}

	set.Language = "ru"
	e = newEngine(fakeData{})
	e.SetLanguage("ru")
	got = byRule(play(e, set, 60, 160, levelUp), "skill_points")
	if len(got) == 0 || got[0].Text != "У вас есть нераспределённое очко умений. Прокачайте способность" {
		t.Fatalf("without a pro order: %+v", got)
	}
}
