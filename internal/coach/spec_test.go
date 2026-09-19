package coach

import (
	"strings"
	"testing"

	"gourdian/internal/config"
	"gourdian/internal/gsi"
)

func customEngine(t *testing.T, specs ...RuleSpec) *Engine {
	t.Helper()
	e := newEngine(fakeData{})
	for _, s := range specs {
		if err := s.Validate(); err != nil {
			t.Fatalf("%s: %v", s.Name, err)
		}
	}
	e.SetCustomRules(specs)
	return e
}

func bkbRule() RuleSpec {
	return RuleSpec{ID: "custom-bkb", Name: "Save for BKB", Enabled: true, Category: "items",
		When: Trigger{Type: WhenState, For: 5}, Match: "all",
		If: []Cond{
			{Field: "clock", Op: "ge", Num: 1080},
			{Field: "gold", Op: "ge", Num: 4050},
			{Field: "has_item", Arg: "black_king_bar", Op: "false"},
		},
		Then:     AlertSpec{Text: "Buy BKB now ({gold}g) at {clock}", Speech: "Buy B K B", Severity: "warn", Mistake: true},
		Cooldown: 60}
}

func TestStateRuleHoldsThenRespectsCooldown(t *testing.T) {
	e := customEngine(t, bkbRule())
	tips := byRule(play(e, settings(config.RoleCarry), 1070, 1160, func(s *gsi.State) { s.Player.Gold = 4200 }), "custom-bkb")
	if len(tips) != 2 || tips[0].Clock != 1085 || tips[1].Clock != 1145 {
		t.Fatalf("tips = %+v", tips)
	}
	if tips[0].Text != "Buy BKB now (4200g) at 18:05" || tips[0].Speech != "Buy B K B" || tips[0].Severity != Warn || !tips[0].Habit {
		t.Fatalf("tip = %+v", tips[0])
	}
	owned := byRule(play(customEngine(t, bkbRule()), settings(config.RoleCarry), 1080, 1120, func(s *gsi.State) {
		s.Player.Gold = 4200
		s.Items["slot0"] = gsi.Item{Name: "item_black_king_bar"}
	}), "custom-bkb")
	if len(owned) != 0 {
		t.Fatal("owning the item should stop the rule")
	}
}

func TestChangeRuleFiresOnEachEdge(t *testing.T) {
	spec := RuleSpec{ID: "custom-low-mana", Name: "Low mana", Enabled: true, Category: "survival",
		When: Trigger{Type: WhenChange}, Match: "any", If: []Cond{{Field: "mana_pct", Op: "lt", Num: 20}},
		Then: AlertSpec{Text: "Mana at {mana_pct}%", Severity: "info", Silent: true}}
	tips := byRule(play(customEngine(t, spec), settings(config.RoleMid), 100, 160, func(s *gsi.State) {
		if s.Map.ClockTime >= 110 && s.Map.ClockTime < 130 || s.Map.ClockTime >= 150 {
			s.Hero.ManaPercent = 10
		}
	}), "custom-low-mana")
	if len(tips) != 2 || tips[0].Clock != 110 || tips[1].Clock != 150 || tips[0].Text != "Mana at 10%" || !tips[0].Quiet {
		t.Fatalf("tips = %+v", tips)
	}
}

func TestEventAndScheduleRules(t *testing.T) {
	died := RuleSpec{ID: "custom-died", Name: "Death shopping", Enabled: true, Category: "survival",
		When: Trigger{Type: WhenEvent, Event: "died"}, Match: "all", If: []Cond{{Field: "gold", Op: "ge", Num: 500}},
		Then: AlertSpec{Text: "Dead for {respawn}s with {gold} gold: buy something", Severity: "urgent"}}
	lotus := RuleSpec{ID: "custom-lotus", Name: "Lotus pool", Enabled: true, Category: "timing",
		When: Trigger{Type: WhenSchedule, First: 180, Every: 180, Lead: 10, Until: 540}, Match: "all",
		Then: AlertSpec{Text: "Lotus at {at}, in {in}s", Severity: "info"}}
	tips := play(customEngine(t, died, lotus), settings(config.RoleSoftSupport), 160, 600, func(s *gsi.State) {
		s.Player.Gold = 700
		if s.Map.ClockTime >= 200 && s.Map.ClockTime < 230 {
			s.Hero.Alive, s.Hero.RespawnSeconds = false, 30-(s.Map.ClockTime-200)
		}
	})
	d := byRule(tips, "custom-died")
	if len(d) != 1 || d[0].Text != "Dead for 30s with 700 gold: buy something" {
		t.Fatalf("death tips = %+v", d)
	}
	l := byRule(tips, "custom-lotus")
	if len(l) != 3 || l[0].Text != "Lotus at 3:00, in 10s" || l[2].Clock != 530 {
		t.Fatalf("lotus tips = %+v", l)
	}
}

func TestSpecValidation(t *testing.T) {
	good := bkbRule()
	for name, mutate := range map[string]func(*RuleSpec){
		"name":       func(r *RuleSpec) { r.Name = " " },
		"cooldown":   func(r *RuleSpec) { r.Cooldown = 0 },
		"field":      func(r *RuleSpec) { r.If[0].Field = "networth" },
		"op":         func(r *RuleSpec) { r.If[2].Op = "gt" },
		"arg":        func(r *RuleSpec) { r.If[2].Arg = "" },
		"event":      func(r *RuleSpec) { r.When = Trigger{Type: WhenEvent, Event: "rampage"} },
		"template":   func(r *RuleSpec) { r.Then.Text = "Buy {networth}" },
		"severity":   func(r *RuleSpec) { r.Then.Severity = "loud" },
		"role":       func(r *RuleSpec) { r.Roles = []string{"jungler"} },
		"schedule":   func(r *RuleSpec) { r.When = Trigger{Type: WhenSchedule, First: 60, Every: 10} },
		"category":   func(r *RuleSpec) { r.Category = "misc" },
		"match mode": func(r *RuleSpec) { r.Match = "some" },
	} {
		r := good
		r.If = append([]Cond(nil), good.If...)
		mutate(&r)
		if r.Validate() == nil {
			t.Errorf("%s: invalid rule accepted", name)
		}
	}
	good.Once, good.Cooldown = true, 0
	if err := good.Validate(); err != nil {
		t.Fatalf("once per match needs no cooldown: %v", err)
	}
}

func TestOverridesChangeBuiltinRules(t *testing.T) {
	e := newEngine(nil)
	edited, _ := DefaultSpec("no_tp")
	edited.When.For, edited.Cooldown = 2, 30
	e.SetOverrides(map[string]RuleOverride{
		"no_tp": {Severity: "urgent", Voice: "silent", Spec: &edited},
		"stash": {Roles: []string{config.RoleHardSupport}},
	})
	tips := play(e, settings(config.RoleCarry), 100, 140, func(s *gsi.State) {
		s.Items["teleport0"] = gsi.Item{Name: "empty"}
		s.Items["stash0"] = gsi.Item{Name: "item_branches"}
		s.Player.Gold = 500
	})
	tp := byRule(tips, "no_tp")
	if len(tp) != 2 || tp[0].Clock != 102 || tp[1].Clock != 132 || tp[0].Severity != Urgent || !tp[0].Quiet {
		t.Fatalf("no_tp tips = %+v", tp)
	}
	if len(byRule(tips, "stash")) != 0 {
		t.Fatal("stash is limited to hard supports by the override")
	}
}

func TestCheckAndTestSpec(t *testing.T) {
	e := customEngine(t)
	set := settings(config.RoleCarry)
	if _, _, err := e.CheckSpec(bkbRule(), set); err == nil {
		t.Fatal("checking needs a match")
	}
	play(e, set, 1100, 1101, func(s *gsi.State) { s.Player.Gold = 3000 })
	results, ok, err := e.CheckSpec(bkbRule(), set)
	if err != nil || ok || len(results) != 3 || results[0].Value != "18:21" || !results[0].OK || results[1].Value != "3000" || results[1].OK || results[2].Value != "no" {
		t.Fatalf("results = %+v %v %v", results, ok, err)
	}
	states := func(yield func(*gsi.State) bool) {
		for clock := 1080; clock < 1200; clock++ {
			s := state(clock)
			s.Player.Gold = 4500
			if !yield(s) {
				return
			}
		}
	}
	tips := TestSpec(fakeData{}, bkbRule(), set, states)
	if len(tips) != 2 || !strings.HasPrefix(tips[0].Text, "Buy BKB now (4500g)") {
		t.Fatalf("test run = %+v", tips)
	}
}

// The rule editor's live check must show the values the rule really gets: it used to leave
// out the player's targets and the previous update, so lh_target read 0 and
// gold_before_death read the gold after the death.
func TestCheckSpecSeesWhatTheEngineSees(t *testing.T) {
	e := newEngine(nil)
	e.SetTargetSource(fixedTargets{LastHits: []int{40, 80, 120, 160, 200}})
	set := settings(config.RoleCarry)
	before := state(599)
	before.Player.Gold, before.Player.GoldReliable = 900, 100
	e.Update(before, set)
	now := state(600)
	now.Player.Gold, now.Player.GoldReliable = 450, 100 // died and lost unreliable gold
	e.Update(now, set)

	spec := RuleSpec{ID: "check", Name: "check", If: []Cond{
		{Field: "lh_target", Op: ">", Num: 0},
		{Field: "gold_before_death", Op: ">", Num: 0},
	}}
	got, _, err := e.CheckSpec(spec, set)
	if err != nil {
		t.Fatal(err)
	}
	if got[0].Value != "80" {
		t.Errorf("lh_target = %s, want 80 from the player's targets", got[0].Value)
	}
	if got[1].Value != "800" {
		t.Errorf("gold_before_death = %s, want 800 from the update before the death", got[1].Value)
	}
}
