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

	"wind_lace":         {ID: 244, DName: "Wind Lace", Cost: 225, Qual: "component"},
	"tranquil_boots":    {ID: 214, DName: "Tranquil Boots", Cost: 900, Qual: "rare", Created: true, Components: []string{"boots", "wind_lace"}},
	"ancient_janggo":    {ID: 185, DName: "Drum of Endurance", Cost: 1625, Qual: "rare", Created: true},
	"boots_of_bearing":  {ID: 596, DName: "Boots of Bearing", Cost: 4225, Qual: "epic", Created: true, Components: []string{"tranquil_boots", "ancient_janggo"}},
	"blade_of_alacrity": {ID: 20, DName: "Blade of Alacrity", Cost: 1000, Qual: "component"},
	"dragon_lance":      {ID: 236, DName: "Dragon Lance", Cost: 1900, Qual: "artifact", Created: true, Components: []string{"blade_of_alacrity", "belt_of_strength"}},
	"force_staff":       {ID: 102, DName: "Force Staff", Cost: 2200, Qual: "rare", Created: true},
	"hurricane_pike":    {ID: 263, DName: "Hurricane Pike", Cost: 4450, Qual: "epic", Created: true, Components: []string{"force_staff", "dragon_lance"}},
	"ultimate_scepter":  {ID: 108, DName: "Aghanim's Scepter", Cost: 4200, Qual: "rare", Created: true},
}

// names lists a build as "phase:item", in order.
func names(b *Build) []string {
	var out []string
	for _, it := range b.Items {
		out = append(out, it.Phase+":"+it.Name)
	}
	return out
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
	b := BuildFromPopularity(1, pop, nil, testItems)
	// Without purchase times a phase reads most bought first.
	want := []string{"start:tango", "start:quelling_blade", "start:ward_observer", "early:power_treads", "mid:bfury"}
	if got := names(b); !slices.Equal(got, want) {
		t.Fatalf("build = %v, want %v", got, want)
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
	}, nil, items)
	var got []string
	for _, it := range b.Items {
		got = append(got, it.Name)
	}
	if slices.Contains(got, "ultimate_orb") || slices.Contains(got, "ogre_axe") || !slices.Contains(got, "blink") || !slices.Contains(got, "skadi") {
		t.Fatalf("after laning the build should hold finished items and Blink, got %v", got)
	}
}

// An item carried for minutes before it is combined away is a step of the build; one bought
// moments before is part of the step that follows it.
func TestBuildKeepsItemsCarriedBeforeTheyAreUpgraded(t *testing.T) {
	b := BuildFromPopularity(6, Popularity{
		"mid_game_items": {"236": 200, "102": 150, "263": 140, "108": 70},
	}, BuyTimes{
		"mid_game_items": {"236": 863, "102": 1210, "263": 1251, "108": 1138},
	}, testItems)
	want := []string{"mid:dragon_lance", "mid:ultimate_scepter", "mid:hurricane_pike"}
	if got := names(b); !slices.Equal(got, want) {
		t.Fatalf("build = %v, want %v", got, want)
	}
}

// A Scepter a third of the games buy must not lead a phase just for costing less than the
// items most of them buy: the order is when pros buy, not what they pay.
func TestBuildOrdersAPhaseByWhenProsBuy(t *testing.T) {
	b := BuildFromPopularity(6, Popularity{
		"mid_game_items": {"108": 70, "263": 140},
	}, BuyTimes{
		"mid_game_items": {"108": 1500, "263": 1251},
	}, testItems)
	want := []string{"mid:hurricane_pike", "mid:ultimate_scepter"}
	if got := names(b); !slices.Equal(got, want) {
		t.Fatalf("build = %v, want %v", got, want)
	}
}

// Boots a support wears all game must survive an upgrade two of the games reached.
func TestBuildKeepsBootsWhoseUpgradeIsRare(t *testing.T) {
	b := BuildFromPopularity(5, Popularity{
		"early_game_items": {"29": 25, "244": 21, "214": 17},
		"late_game_items":  {"596": 2},
	}, BuyTimes{
		"early_game_items": {"29": 327, "244": 392, "214": 482},
		"late_game_items":  {"596": 1700},
	}, testItems)
	want := []string{"early:tranquil_boots", "late:boots_of_bearing"}
	if got := names(b); !slices.Equal(got, want) {
		t.Fatalf("build = %v, want %v", got, want)
	}
}

// The parts of an item take no room from the phase that holds it, so the items further down
// the list are reached instead of being cut for components that are dropped anyway.
func TestBuildSpendsAPhaseOnItemsThatSurviveIt(t *testing.T) {
	b := BuildFromPopularity(1, Popularity{
		"early_game_items": {"29": 100, "25": 95, "13": 90, "63": 85, "11": 80, "56": 75, "57": 70, "69": 65},
	}, nil, testItems)
	want := []string{"early:power_treads", "early:quelling_blade", "early:pers"}
	if got := names(b); !slices.Equal(got, want) {
		t.Fatalf("build = %v, want %v", got, want)
	}
}

// A Ghost Scepter is sold finished, so it belongs in a build even though an Ethereal Blade is
// made of one. Only the buyers who went on to the Blade right away were on their way to it.
func TestBuildKeepsAGhostScepterBoughtForItself(t *testing.T) {
	items := map[string]ItemInfo{
		"ghost":          {ID: 37, DName: "Ghost Scepter", Cost: 1500, Qual: "component"},
		"cyclone":        {ID: 100, DName: "Eul's Scepter", Cost: 2725, Qual: "rare", Created: true},
		"ethereal_blade": {ID: 176, DName: "Ethereal Blade", Cost: 5200, Qual: "epic", Created: true, Components: []string{"ghost"}},
	}
	b := BuildFromPopularity(26, Popularity{
		"mid_game_items":  {"37": 50, "100": 40},
		"late_game_items": {"176": 30},
	}, BuyTimes{
		"mid_game_items":  {"37": 1000, "100": 1100},
		"late_game_items": {"176": 2000},
	}, items)
	want := []string{"mid:ghost", "mid:cyclone", "late:ethereal_blade"}
	if got := names(b); !slices.Equal(got, want) {
		t.Fatalf("build = %v, want %v", got, want)
	}
}
