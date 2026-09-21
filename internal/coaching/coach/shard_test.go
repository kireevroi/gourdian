package coach

import (
	"slices"
	"testing"

	"gourdian/internal/data/opendota"
	"gourdian/internal/game/dota"
	"gourdian/internal/game/gsi"
	"gourdian/internal/sys/config"
)

// shardSpec is a rule over the Shard values, the way a player's rule would use them.
var shardSpec = RuleSpec{ID: "custom-shard", Name: "Shard", Enabled: true, Category: "items", Match: "all",
	When: Trigger{Type: WhenState, For: 30},
	If: []Cond{
		{Field: "shard_on_sale", Op: "true"},
		{Field: "shard_gold", Op: "eq"},
	},
	Then:     AlertSpec{Text: "Buy Aghanim's Shard ({shard_cost}g)", Severity: "warn"},
	Cooldown: 300}

func shardEngine(t *testing.T, cost int) *Engine {
	t.Helper()
	e := newEngine(fakeData{items: map[string]opendota.ItemInfo{
		"aghanims_shard": {DName: "Aghanim's Shard", Cost: cost},
	}})
	if err := shardSpec.Validate(); err != nil {
		t.Fatal(err)
	}
	e.SetCustomRules([]RuleSpec{shardSpec})
	return e
}

// The Shard is on sale from 15:00, and its price comes from OpenDota's item data.
func TestShardValuesFollowTheClockAndTheGold(t *testing.T) {
	from := dota.DefaultTimings().ShardFrom
	rich := func(s *gsi.State) { s.Player.Gold = 1500 }
	got := byRule(play(shardEngine(t, 1400), settings(dota.HardSupport), from, from+60, rich), "custom-shard")
	if len(got) != 1 || got[0].Clock != from+30 {
		t.Fatalf("want one alert 30 seconds after 15:00, got %+v", got)
	}
	if got[0].Text != "Buy Aghanim's Shard (1400g)" {
		t.Errorf("text = %q", got[0].Text)
	}
}

// A Shard from a Tormentor takes it off sale, and so does being short of the price or early.
func TestShardIsNotOnSaleWhenItShouldntBe(t *testing.T) {
	from := dota.DefaultTimings().ShardFrom
	for _, c := range []struct {
		name     string
		from, to int
		mutate   func(*gsi.State)
	}{
		{"already holding one", from, from + 60, func(s *gsi.State) { s.Player.Gold = 5000; s.Hero.AghanimsShard = true }},
		{"a gold short", from, from + 60, func(s *gsi.State) { s.Player.Gold = 1399 }},
		{"before it goes on sale", from - 120, from - 1, func(s *gsi.State) { s.Player.Gold = 5000 }},
	} {
		if got := byRule(play(shardEngine(t, 1400), settings(dota.HardSupport), c.from, c.to, c.mutate), "custom-shard"); len(got) > 0 {
			t.Errorf("%s: %+v", c.name, got)
		}
	}
}

// A repriced Shard needs no release: the cost and the gold still needed follow the item data.
func TestShardCostFollowsTheItemData(t *testing.T) {
	from := dota.DefaultTimings().ShardFrom
	rich := func(s *gsi.State) { s.Player.Gold = 1500 }
	got := byRule(play(shardEngine(t, 1600), settings(dota.HardSupport), from, from+60, rich), "custom-shard")
	if len(got) > 0 {
		t.Fatalf("1500 gold shouldn't cover a 1600 gold Shard: %+v", got)
	}
	got = byRule(play(shardEngine(t, 1200), settings(dota.HardSupport), from, from+60, rich), "custom-shard")
	if len(got) != 1 || got[0].Text != "Buy Aghanim's Shard (1200g)" {
		t.Fatalf("got %+v", got)
	}
}

// Without OpenDota's prices the values still work, at the price the app ships with.
func TestShardCostFallsBackToTheShippedPrice(t *testing.T) {
	e := newEngine(nil)
	e.SetCustomRules([]RuleSpec{shardSpec})
	from := dota.DefaultTimings().ShardFrom
	got := byRule(play(e, settings(dota.HardSupport), from, from+60, func(s *gsi.State) { s.Player.Gold = 1500 }), "custom-shard")
	if len(got) != 1 || got[0].Text != "Buy Aghanim's Shard (1400g)" {
		t.Fatalf("got %+v", got)
	}
}

var shardItems = map[string]opendota.ItemInfo{
	"aghanims_shard": {DName: "Aghanim's Shard", Cost: 1400},
	"force_staff":    {DName: "Force Staff", Cost: 2200},
}

// shardBuild is a hero whose popular build buys a Shard, such as Crystal Maiden.
var shardBuild = fakeData{items: shardItems, build: &opendota.Build{Items: []opendota.BuildItem{
	{Name: "aghanims_shard", DName: "Aghanim's Shard", Cost: 1400, Phase: opendota.PhaseMid},
	{Name: "force_staff", DName: "Force Staff", Cost: 2200, Phase: opendota.PhaseMid},
}}}

// plainBuild is a hero whose popular build has no Shard in it, such as Lion.
var plainBuild = fakeData{items: shardItems, build: &opendota.Build{Items: []opendota.BuildItem{
	{Name: "force_staff", DName: "Force Staff", Cost: 2200, Phase: opendota.PhaseMid},
}}}

// shardOn is the player having turned the Shard rules on, which the trainer does not ship with.
func shardOn(role string) config.Settings {
	set := settings(role)
	set.DisabledRules = slices.DeleteFunc(slices.Clone(set.DisabledRules), func(id string) bool {
		return slices.Contains(config.RulesShipOff, id)
	})
	return set
}

// The Shard is a matter of taste, not a mistake, so neither rule says anything unasked.
func TestShardRulesShipSwitchedOff(t *testing.T) {
	from := dota.DefaultTimings().ShardFrom
	rich := func(s *gsi.State) { s.Player.Gold = 1500 }
	tips := play(newEngine(shardBuild), settings(dota.HardSupport), from-30, from+120, rich)
	for _, rule := range []string{"shard_sale", "shard"} {
		if got := byRule(tips, rule); len(got) > 0 {
			t.Errorf("%s spoke without being turned on: %+v", rule, got)
		}
	}
}

// The Shard goes on sale at 15:00, and a player who doesn't have one hears about it once.
func TestShardSaleIsAnnouncedOnce(t *testing.T) {
	from := dota.DefaultTimings().ShardFrom
	got := byRule(play(newEngine(shardBuild), shardOn(dota.Carry), from-30, from+300, nil), "shard_sale")
	if len(got) != 1 || got[0].Clock != from {
		t.Fatalf("want one announcement at 15:00, got %+v", got)
	}
	if got[0].Text != "Aghanim's Shard is on sale (1400g). It upgrades one of your abilities for the rest of the game" {
		t.Errorf("text = %q", got[0].Text)
	}
}

// A Shard from a Tormentor is still a Shard, so there is nothing to announce and nothing to buy.
func TestShardRulesAreQuietWhenYouAlreadyHaveOne(t *testing.T) {
	from := dota.DefaultTimings().ShardFrom
	got := play(newEngine(shardBuild), shardOn(dota.HardSupport), from-30, from+60, func(s *gsi.State) {
		s.Hero.AghanimsShard = true
		s.Player.Gold = 1500
	})
	for _, rule := range []string{"shard_sale", "shard"} {
		if tips := byRule(got, rule); len(tips) > 0 {
			t.Errorf("%s spoke to a player already holding a Shard: %+v", rule, tips)
		}
	}
}

// The nudge follows the build, not the position: a hero whose build buys a Shard gets it,
// core or support, and a hero whose build has no Shard is left alone however rich they are.
func TestShardNudgeFollowsTheBuild(t *testing.T) {
	from := dota.DefaultTimings().ShardFrom
	rich := func(s *gsi.State) { s.Player.Gold = 1500 }
	for _, role := range []string{dota.HardSupport, dota.Carry} {
		got := byRule(play(newEngine(shardBuild), shardOn(role), from, from+60, rich), "shard")
		if len(got) != 1 || got[0].Clock != from+30 {
			t.Fatalf("%s: want one nudge 30 seconds in, got %+v", role, got)
		}
		if got[0].Text != "You can afford Aghanim's Shard (1400g), and your build buys one" {
			t.Errorf("%s: text = %q", role, got[0].Text)
		}
		if !got[0].Habit {
			t.Errorf("%s: a Shard the build wants and nobody bought should count as a mistake", role)
		}
	}
	for _, role := range []string{dota.HardSupport, dota.SoftSupport, dota.Carry} {
		if got := byRule(play(newEngine(plainBuild), shardOn(role), from, from+120, rich), "shard"); len(got) > 0 {
			t.Errorf("%s: nudged a hero whose build has no Shard: %+v", role, got)
		}
	}
}

// With no build to vet it against there is nothing to say either.
func TestShardNudgeWaitsForTheBuild(t *testing.T) {
	from := dota.DefaultTimings().ShardFrom
	rich := func(s *gsi.State) { s.Player.Gold = 1500 }
	if got := byRule(play(newEngine(nil), shardOn(dota.HardSupport), from, from+120, rich), "shard"); len(got) > 0 {
		t.Errorf("nudged before any build had loaded: %+v", got)
	}
}

// Short of the price, or before it goes on sale, the nudge stays out of the way.
func TestShardNudgeWaitsForTheGoldAndTheClock(t *testing.T) {
	from := dota.DefaultTimings().ShardFrom
	poor := func(s *gsi.State) { s.Player.Gold = 1399 }
	if got := byRule(play(newEngine(shardBuild), shardOn(dota.HardSupport), from, from+60, poor), "shard"); len(got) > 0 {
		t.Errorf("nudged a player 1 gold short: %+v", got)
	}
	rich := func(s *gsi.State) { s.Player.Gold = 5000 }
	if got := byRule(play(newEngine(shardBuild), shardOn(dota.HardSupport), from-120, from-1, rich), "shard"); len(got) > 0 {
		t.Errorf("nudged a player before the Shard was on sale: %+v", got)
	}
}

// A Shard is spent on the hero, so no slot holds it. The build has to move on all the same.
func TestBoughtShardCountsTowardsTheBuild(t *testing.T) {
	from := dota.DefaultTimings().ShardFrom
	// next_item speaks when the gold arrives, so the player starts the run short of it.
	earns := func(shard bool) func(*gsi.State) {
		return func(s *gsi.State) {
			s.Hero.AghanimsShard = shard
			if s.Map.ClockTime > from {
				s.Player.Gold = 2500
			}
		}
	}
	got := byRule(play(newEngine(shardBuild), settings(dota.HardSupport), from, from+30, earns(false)), "next_item")
	if len(got) != 1 || got[0].Text != "You can afford Aghanim's Shard" {
		t.Fatalf("want the Shard as the next item, got %+v", got)
	}
	got = byRule(play(newEngine(shardBuild), settings(dota.HardSupport), from, from+30, earns(true)), "next_item")
	if len(got) != 1 || got[0].Text != "You can afford Force Staff" {
		t.Fatalf("a bought Shard should move the build on to Force Staff, got %+v", got)
	}
}
