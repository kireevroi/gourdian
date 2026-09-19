package dotadata

import (
	"slices"
	"testing"
)

var testItems = map[string]ItemInfo{
	"boots":            {ID: 29, DName: "Boots of Speed", Cost: 500, Qual: "common"},
	"gloves":           {ID: 25, DName: "Gloves of Haste", Cost: 450, Qual: "component"},
	"belt_of_strength": {ID: 13, DName: "Belt of Strength", Cost: 450, Qual: "component"},
	"power_treads":     {ID: 63, DName: "Power Treads", Cost: 1400, Components: []string{"boots", "gloves", "belt_of_strength"}},
	"quelling_blade":   {ID: 11, DName: "Quelling Blade", Cost: 100, Qual: "component"},
	"broadsword":       {ID: 3, DName: "Broadsword", Cost: 1000, Qual: "component"},
	"pers":             {ID: 69, DName: "Perseverance", Cost: 1400, Components: []string{"ring_of_health", "void_stone"}},
	"ring_of_health":   {ID: 56, DName: "Ring of Health", Cost: 700},
	"void_stone":       {ID: 57, DName: "Void Stone", Cost: 700},
	"bfury":            {ID: 145, DName: "Battle Fury", Cost: 3900, Components: []string{"pers", "broadsword", "broadsword", "quelling_blade"}},
	"tango":            {ID: 44, DName: "Tango", Cost: 90, Qual: "consumable"},
	"ward_observer":    {ID: 42, DName: "Observer Ward", Cost: 0, Qual: "consumable"},
	"recipe_bfury":     {ID: 146, DName: "Recipe", Cost: 0},
	"occult_bracelet":  {ID: 1575, DName: "Occult Bracelet", Tier: 1},
}

func TestRemainingCost(t *testing.T) {
	cases := []struct {
		target string
		held   []string
		want   int
	}{
		{"power_treads", nil, 1400},
		{"power_treads", []string{"boots"}, 900},
		{"power_treads", []string{"boots", "gloves", "belt_of_strength"}, 0},
		{"bfury", []string{"quelling_blade", "ring_of_health"}, 3100},
		{"bfury", []string{"broadsword"}, 2900},
		{"bfury", []string{"bfury"}, 3900},
	}
	for _, c := range cases {
		if got := RemainingCost(c.target, c.held, testItems); got != c.want {
			t.Errorf("RemainingCost(%s, %v) = %d, want %d", c.target, c.held, got, c.want)
		}
	}
}

func TestOwnedClosureIncludesComponents(t *testing.T) {
	owned := OwnedClosure([]string{"bfury"}, testItems)
	for _, n := range []string{"bfury", "pers", "ring_of_health", "broadsword", "quelling_blade"} {
		if !owned[n] {
			t.Errorf("%s should count as owned", n)
		}
	}
	if owned["power_treads"] {
		t.Error("power_treads should not be owned")
	}
}

func TestBuildFromPopularity(t *testing.T) {
	pop := Popularity{
		"start_game_items": {"44": 90, "11": 80, "42": 70},
		"early_game_items": {"29": 100, "63": 90, "44": 60, "56": 10},
		"mid_game_items":   {"145": 95, "63": 20, "1575": 90, "146": 99},
		"late_game_items":  {},
	}
	b := BuildFromPopularity(1, pop, testItems)
	var names []string
	for _, it := range b.Items {
		names = append(names, it.Phase+":"+it.Name)
	}
	want := []string{"start:ward_observer", "start:tango", "start:quelling_blade", "early:power_treads", "mid:bfury"}
	if !slices.Equal(names, want) {
		t.Fatalf("build = %v, want %v", names, want)
	}

	next, ok := b.Next([]string{"power_treads"}, testItems, 300)
	if !ok || next.Name != "bfury" {
		t.Fatalf("next after treads = %+v, want bfury", next)
	}
	if next, ok := b.Next([]string{"power_treads", "bfury"}, testItems, 300); ok {
		t.Fatalf("expected build complete, got %+v", next)
	}
	if next, _ := b.Next([]string{"bfury"}, testItems, 300); next.Name == "power_treads" {
		t.Fatal("skipped early item should not block once a later item is owned")
	}
	if next, _ := b.Next(nil, testItems, 1000); next.Name != "bfury" {
		t.Fatalf("early items should be skipped after the early game, got %+v", next)
	}
}

func TestLaterPhasesListFinishedItems(t *testing.T) {
	items := map[string]ItemInfo{
		"ultimate_orb":   {ID: 1, DName: "Ultimate Orb", Cost: 2800, Qual: "secret_shop"},
		"ogre_axe":       {ID: 2, DName: "Ogre Axe", Cost: 1000, Qual: "component"},
		"blink":          {ID: 3, DName: "Blink Dagger", Cost: 2250, Qual: "component"},
		"black_king_bar": {ID: 4, DName: "Black King Bar", Cost: 4050, Qual: "epic", Created: true, Components: []string{"ogre_axe"}},
		"skadi":          {ID: 5, DName: "Eye of Skadi", Cost: 5900, Qual: "artifact", Created: true, Components: []string{"ultimate_orb"}},
	}
	b := BuildFromPopularity(17, Popularity{
		"mid_game_items":  {"2": 50, "3": 40, "4": 30},
		"late_game_items": {"1": 50, "5": 40},
	}, items)
	var got []string
	for _, it := range b.Items {
		got = append(got, it.Name)
	}
	if slices.Contains(got, "ultimate_orb") || slices.Contains(got, "ogre_axe") || !slices.Contains(got, "blink") || !slices.Contains(got, "skadi") {
		t.Fatalf("after laning the build should hold finished items and Blink, got %v", got)
	}
}
