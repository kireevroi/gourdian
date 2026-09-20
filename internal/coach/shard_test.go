package coach

import (
	"testing"

	"gourdian/internal/dota"
	"gourdian/internal/dotadata"
	"gourdian/internal/gsi"
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
	e := newEngine(fakeData{items: map[string]dotadata.ItemInfo{
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
