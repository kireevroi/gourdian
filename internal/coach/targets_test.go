package coach

import (
	"slices"
	"strings"
	"testing"
	"time"

	"gourdian/internal/config"
	"gourdian/internal/dotadata"
	"gourdian/internal/gsi"
	"gourdian/internal/stats"
)

func TestPersonalLastHits(t *testing.T) {
	lh := func(v map[string]int) stats.MatchSummary { return stats.MatchSummary{LastHitsAt: v} }
	history := []stats.MatchSummary{
		lh(map[string]int{"5:00": 20, "10:00": 40}), lh(map[string]int{"10:00": 44}),
		lh(map[string]int{"10:00": 38}), lh(map[string]int{"5:00": 25, "10:00": 50}),
	}
	got := PersonalLastHits(config.RoleCarry, history)
	// 10:00: median of 38, 40, 44, 50 is 42, so the target is 46. 5:00 has only two games.
	if !slices.Equal(got.LastHits, []int{30, 46, 110, 160, 270}) || got.Usual[1] != 42 || got.Games != 4 {
		t.Fatalf("targets = %+v", got)
	}
	high := PersonalLastHits(config.RoleCarry, []stats.MatchSummary{
		lh(map[string]int{"5:00": 40}), lh(map[string]int{"5:00": 40}), lh(map[string]int{"5:00": 40}),
	})
	if high.LastHits[0] != 44 || high.LastHits[1] != 65 {
		t.Fatalf("5:00 target 44 and 10:00 from the table: %+v", high.LastHits)
	}
	if PersonalLastHits(config.RoleHardSupport, history).LastHits != nil {
		t.Fatal("supports have no last-hit targets")
	}
}

type fixedTargets Targets

func (f fixedTargets) TargetsFor(int, string) Targets { return Targets(f) }

func TestItemTimingGoal(t *testing.T) {
	items := map[string]dotadata.ItemInfo{"bfury": {DName: "Battle Fury", Cost: 4100}, "mjollnir": {DName: "Mjollnir", Cost: 5500}}
	goal := fixedTargets{LastHits: RoleTargets(config.RoleCarry).LastHits, Items: []ItemGoal{{Item: "bfury", Name: "Battle Fury", By: 900}}}
	runRule := func(rule string, mutate func(*gsi.State)) []Tip {
		e := newEngine(fakeData{items: items})
		e.SetTargetSource(goal)
		return byRule(play(e, settings(config.RoleCarry), 0, 1000, mutate), rule)
	}
	run := func(mutate func(*gsi.State)) []Tip { return runRule("item_timing", mutate) }
	soon := run(nil)
	if len(soon) != 1 || soon[0].Clock != 780 || !strings.HasPrefix(soon[0].Text, "Battle Fury by 15:00:") {
		t.Fatalf("heads-up tips = %+v", soon)
	}
	late := runRule("item_late", nil)
	if len(late) != 1 || late[0].Clock != 960 || !late[0].Habit {
		t.Fatalf("late tips = %+v", late)
	}
	onTime := runRule("item_late", func(s *gsi.State) {
		if s.Map.ClockTime >= 850 {
			s.Items["slot0"] = gsi.Item{Name: "item_bfury"}
		}
	})
	if len(onTime) != 0 {
		t.Fatalf("finishing the item should stop the late warning: %+v", onTime)
	}
	switched := runRule("item_late", func(s *gsi.State) {
		if s.Map.ClockTime >= 700 {
			s.Items["slot1"] = gsi.Item{Name: "item_mjollnir"}
		}
	})
	if len(switched) != 0 {
		t.Fatalf("a different big item means a changed build, not a late one: %+v", switched)
	}
}

func TestSnapshotShowsPersonalPaceAndItemGoals(t *testing.T) {
	items := map[string]dotadata.ItemInfo{"bfury": {DName: "Battle Fury", Cost: 4100}}
	e := newEngine(fakeData{items: items})
	e.SetTargetSource(fixedTargets{LastHits: []int{33, 72, 110, 160, 270}, Usual: []int{30, 65, 0, 0, 0}, Items: []ItemGoal{{Item: "bfury", Name: "Battle Fury", By: 900}}})
	play(e, settings(config.RoleCarry), 400, 401, nil)
	snap := e.Snapshot(settings(config.RoleCarry))
	if snap.Pace == nil || snap.Pace.Target != 72 || snap.Pace.Usual != 65 || len(snap.ItemGoals) != 1 || snap.ItemGoals[0].Remaining != 4100 {
		t.Fatalf("pace %+v goals %+v", snap.Pace, snap.ItemGoals)
	}
}

// The item timing goals used their own median, which took the upper middle value; every
// target now uses the same one.
func TestMedian(t *testing.T) {
	for _, c := range []struct {
		in   []int
		want int
	}{{nil, 0}, {[]int{7}, 7}, {[]int{3, 1, 2}, 2}, {[]int{600, 900}, 750}, {[]int{4, 1, 3, 2}, 2}} {
		if got := Median(c.in); got != c.want {
			t.Errorf("Median(%v) = %d, want %d", c.in, got, c.want)
		}
	}
}

type slowTargets struct{ release chan struct{} }

func (s slowTargets) TargetsFor(int, string) Targets {
	<-s.release // working targets out from the data file
	return RoleTargets(config.RoleCarry)
}

// Working out targets can read the data file; the dashboard and HUD mustn't wait for it on
// the engine's lock.
func TestTargetsAreAskedForOutsideTheEngineLock(t *testing.T) {
	e := newEngine(nil)
	src := slowTargets{release: make(chan struct{})}
	e.SetTargetSource(src)
	done := make(chan struct{})
	go func() {
		e.Update(state(60), settings(config.RoleCarry))
		close(done)
	}()
	time.Sleep(50 * time.Millisecond) // Update is waiting for the targets now
	answered := make(chan struct{})
	go func() {
		e.RecentTips()
		close(answered)
	}()
	select {
	case <-answered:
	case <-time.After(2 * time.Second):
		t.Error("the engine was locked while the targets were worked out")
	}
	close(src.release)
	<-done
}
