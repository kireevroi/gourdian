package coach

import (
	"testing"

	"gourdian/internal/dota"
	"gourdian/internal/dotadata"
	"gourdian/internal/gsi"
)

// shardPrices is the item data the trainer would have from OpenDota.
var shardPrices = fakeData{items: map[string]dotadata.ItemInfo{
	"aghanims_shard": {DName: "Aghanim's Shard", Cost: 1400},
}}

// The Shard goes on sale at 15:00, and a player who doesn't have one hears about it once.
func TestShardSaleIsAnnouncedOnce(t *testing.T) {
	from := dota.DefaultTimings().ShardFrom
	got := byRule(play(newEngine(shardPrices), settings(dota.Carry), from-30, from+300, nil), "shard_sale")
	if len(got) != 1 || got[0].Clock != from {
		t.Fatalf("want one announcement at 15:00, got %+v", got)
	}
	if got[0].Text != "Aghanim's Shard is on sale (1400g). It upgrades one of your abilities for the rest of the game" {
		t.Errorf("text = %q", got[0].Text)
	}
}

// A Shard from a Tormentor is still a Shard, so there's nothing to announce.
func TestShardSaleIsQuietWhenYouAlreadyHaveOne(t *testing.T) {
	from := dota.DefaultTimings().ShardFrom
	got := play(newEngine(shardPrices), settings(dota.Carry), from-30, from+60, func(s *gsi.State) {
		s.Hero.AghanimsShard = true
	})
	if tips := byRule(got, "shard_sale"); len(tips) > 0 {
		t.Errorf("announced a sale to a player holding a Shard: %+v", tips)
	}
	if tips := byRule(got, "shard"); len(tips) > 0 {
		t.Errorf("asked a player holding a Shard to buy one: %+v", tips)
	}
}

// A support sitting on the gold for a Shard is told to spend it.
func TestSupportWithShardGoldIsTold(t *testing.T) {
	from := dota.DefaultTimings().ShardFrom
	rich := func(s *gsi.State) { s.Player.Gold = 1500 }
	got := byRule(play(newEngine(shardPrices), settings(dota.HardSupport), from, from+60, rich), "shard")
	if len(got) != 1 || got[0].Clock != from+30 {
		t.Fatalf("want one nudge 30 seconds in, got %+v", got)
	}
	if got[0].Text != "You can afford Aghanim's Shard (1400g). Buy it before your next item" {
		t.Errorf("text = %q", got[0].Text)
	}
	if !got[0].Habit {
		t.Error("a Shard nobody buys should count as a mistake")
	}
}

// Short of the price, or before it goes on sale, the trainer stays out of the way.
func TestShardNudgeWaitsForTheGoldAndTheClock(t *testing.T) {
	from := dota.DefaultTimings().ShardFrom
	poor := func(s *gsi.State) { s.Player.Gold = 1399 }
	if got := byRule(play(newEngine(shardPrices), settings(dota.HardSupport), from, from+60, poor), "shard"); len(got) > 0 {
		t.Errorf("nudged a support 1 gold short: %+v", got)
	}
	rich := func(s *gsi.State) { s.Player.Gold = 5000 }
	if got := byRule(play(newEngine(shardPrices), settings(dota.HardSupport), from-120, from-1, rich), "shard"); len(got) > 0 {
		t.Errorf("nudged a support before the Shard was on sale: %+v", got)
	}
}

// Without OpenDota's prices the rule still works, at the price the app ships with.
func TestShardCostFallsBackToTheShippedPrice(t *testing.T) {
	from := dota.DefaultTimings().ShardFrom
	got := byRule(play(newEngine(nil), settings(dota.Carry), from, from+10, nil), "shard_sale")
	if len(got) != 1 || got[0].Text != "Aghanim's Shard is on sale (1400g). It upgrades one of your abilities for the rest of the game" {
		t.Fatalf("got %+v", got)
	}
}
