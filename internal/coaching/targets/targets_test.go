package targets

import (
	"errors"
	"testing"
	"time"

	"gourdian/internal/coaching/coach"
	"gourdian/internal/data/opendota"
	"gourdian/internal/game/dota"
	"gourdian/internal/game/model"
)

type fakeHistory struct {
	version int64
	matches []model.MatchSummary
	items   []model.ItemTiming
	err     error
	asked   int
}

func (f *fakeHistory) HistoryVersion() int64 { return f.version }

func (f *fakeHistory) RecentOn(int, string, int) ([]model.MatchSummary, error) {
	f.asked++
	return f.matches, f.err
}

func (f *fakeHistory) ItemsIn([]string) ([]model.ItemTiming, error) { return f.items, nil }

type fakeBuilds struct {
	items   map[string]opendota.ItemInfo
	build   *opendota.Build
	timings map[string][]opendota.ItemTiming
}

func (f *fakeBuilds) Items() map[string]opendota.ItemInfo { return f.items }

func (f *fakeBuilds) BuildFor(int, string) *opendota.Build { return f.build }

func (f *fakeBuilds) ItemTimings(_ int, item string) ([]opendota.ItemTiming, bool) {
	b, ok := f.timings[item]
	return b, ok
}

func catalogue() map[string]opendota.ItemInfo {
	return map[string]opendota.ItemInfo{
		"bfury": {DName: "Battle Fury", Cost: 4100},
		"manta": {DName: "Manta Style", Cost: 4650},
		"tango": {DName: "Tango", Cost: 90},
	}
}

func playedCarry(n int) []model.MatchSummary {
	var out []model.MatchSummary
	for i := range n {
		out = append(out, model.MatchSummary{MatchID: string(rune('a' + i)), HeroID: 1, Role: dota.Carry,
			Source: model.SourceLive, LastHitsAt: map[string]int{"10:00": 40}})
	}
	return out
}

func boughtIn(ids []string, item string, at int) []model.ItemTiming {
	var out []model.ItemTiming
	for _, id := range ids {
		out = append(out, model.ItemTiming{MatchID: id, Item: item, Time: at})
	}
	return out
}

func TestTheAnswerIsCachedUntilTheHistoryChanges(t *testing.T) {
	h := &fakeHistory{version: 1, matches: playedCarry(3)}
	c := &Cache{History: h}

	c.TargetsFor(1, dota.HardSupport)
	c.TargetsFor(1, dota.HardSupport)
	if h.asked != 1 {
		t.Fatalf("the history was read %d times, want 1", h.asked)
	}
	h.version = 2
	c.TargetsFor(1, dota.HardSupport)
	if h.asked != 2 {
		t.Fatalf("a new history version should be read again, got %d reads", h.asked)
	}
}

func TestForgetMakesTheNextAskRebuild(t *testing.T) {
	h := &fakeHistory{version: 1, matches: playedCarry(3)}
	c := &Cache{History: h}
	c.TargetsFor(1, dota.HardSupport)
	c.Forget()
	c.TargetsFor(1, dota.HardSupport)
	if h.asked != 2 {
		t.Fatalf("Forget should drop the cached answer, got %d reads", h.asked)
	}
}

func TestEachHeroAndRoleIsCachedApart(t *testing.T) {
	h := &fakeHistory{version: 1, matches: playedCarry(3)}
	c := &Cache{History: h}
	c.TargetsFor(1, dota.HardSupport)
	c.TargetsFor(2, dota.HardSupport)
	c.TargetsFor(1, dota.Mid)
	if h.asked != 3 {
		t.Fatalf("hero and role should key the cache separately, got %d reads", h.asked)
	}
}

// age pushes every cached answer past the retry window, so the next ask rebuilds it.
func age(c *Cache) {
	c.mu.Lock()
	for k, e := range c.entries {
		e.at = e.at.Add(-incompleteWait - time.Second)
		c.entries[k] = e
	}
	c.mu.Unlock()
}

func TestAStoreThatFailsFallsBackToTheRoleDefaults(t *testing.T) {
	h := &fakeHistory{version: 1, err: errors.New("data file is locked")}
	c := &Cache{History: h}
	got := c.TargetsFor(1, dota.Carry)
	if want := coach.RoleTargets(dota.Carry); len(got.LastHits) != len(want.LastHits) {
		t.Fatalf("targets = %+v, want the role defaults %+v", got, want)
	}
	c.TargetsFor(1, dota.Carry)
	if h.asked != 1 {
		t.Fatalf("a fresh failure is reused briefly rather than hammering the store, got %d reads", h.asked)
	}
	age(c)
	c.TargetsFor(1, dota.Carry)
	if h.asked != 2 {
		t.Fatalf("past the retry window the store should be read again, got %d reads", h.asked)
	}
}

func TestSupportsGetNoItemGoals(t *testing.T) {
	h := &fakeHistory{version: 1, matches: playedCarry(5)}
	builds := &fakeBuilds{items: catalogue()}
	c := &Cache{History: h, Builds: builds}
	if got := c.TargetsFor(1, dota.HardSupport); len(got.Items) != 0 {
		t.Fatalf("a support should get no item goals, got %+v", got.Items)
	}
}

func TestYourOwnItemsBeatTheProBuild(t *testing.T) {
	ids := []string{"a", "b", "c"}
	h := &fakeHistory{version: 1, matches: playedCarry(3)}
	h.items = append(boughtIn(ids, "bfury", 1200), boughtIn(ids, "manta", 1800)...)
	builds := &fakeBuilds{items: catalogue(),
		build: &opendota.Build{Items: []opendota.BuildItem{{Name: "manta", Cost: 4650, Phase: opendota.PhaseMid}}}}
	c := &Cache{History: h, Builds: builds}

	got := c.TargetsFor(1, dota.Carry)
	if got.ItemGames != 3 {
		t.Fatalf("goals should come from the player's 3 games, got ItemGames=%d", got.ItemGames)
	}
	if len(got.Items) != 2 || got.Items[0].Item != "bfury" {
		t.Fatalf("item goals = %+v", got.Items)
	}
	// A minute under the usual 1200, rounded to the nearest 30 seconds.
	if got.Items[0].By != 1140 || got.Items[0].Usual != 1200 {
		t.Fatalf("Battle Fury goal = %+v, want By 1140 and Usual 1200", got.Items[0])
	}
}

func TestCheapItemsAreNotGoals(t *testing.T) {
	ids := []string{"a", "b", "c"}
	h := &fakeHistory{version: 1, matches: playedCarry(3), items: boughtIn(ids, "tango", 60)}
	builds := &fakeBuilds{items: catalogue(), build: &opendota.Build{}}
	c := &Cache{History: h, Builds: builds}
	if got := c.TargetsFor(1, dota.Carry); len(got.Items) != 0 {
		t.Fatalf("a 90-gold item is below GoalItemCost, got %+v", got.Items)
	}
}

func TestWithoutEnoughGamesTheProBuildIsUsed(t *testing.T) {
	h := &fakeHistory{version: 1, matches: playedCarry(2), items: boughtIn([]string{"a", "b"}, "bfury", 1200)}
	builds := &fakeBuilds{items: catalogue(),
		build:   &opendota.Build{Items: []opendota.BuildItem{{Name: "manta", Cost: 4650, Phase: opendota.PhaseMid}}},
		timings: map[string][]opendota.ItemTiming{"manta": {{Time: 1500, Games: 40, Wins: 24}}}}
	c := &Cache{History: h, Builds: builds}

	got := c.TargetsFor(1, dota.Carry)
	if got.ItemGames != 0 {
		t.Fatalf("two games are too few to be your own build, got ItemGames=%d", got.ItemGames)
	}
	if len(got.Items) != 1 || got.Items[0].Item != "manta" || got.Items[0].By != 1500 {
		t.Fatalf("item goals = %+v, want Manta at the winning timing", got.Items)
	}
}

func TestALoadingBuildIsRetriedButACompleteOneIsKept(t *testing.T) {
	h := &fakeHistory{version: 1, matches: playedCarry(2)}
	builds := &fakeBuilds{items: catalogue(), build: &opendota.Build{Loading: true}}
	c := &Cache{History: h, Builds: builds}
	c.TargetsFor(1, dota.Carry)
	age(c)
	c.TargetsFor(1, dota.Carry)
	if h.asked != 2 {
		t.Fatalf("an answer made while the build was loading should be retried, got %d reads", h.asked)
	}

	builds.build = &opendota.Build{Items: []opendota.BuildItem{{Name: "manta", Cost: 4650, Phase: opendota.PhaseMid}}}
	builds.timings = map[string][]opendota.ItemTiming{"manta": {{Time: 1500, Games: 40, Wins: 24}}}
	h.version = 2
	c.TargetsFor(1, dota.Carry)
	before := h.asked
	age(c)
	c.TargetsFor(1, dota.Carry)
	if h.asked != before {
		t.Fatalf("a complete answer should outlive the retry window, got %d reads", h.asked-before)
	}
}
