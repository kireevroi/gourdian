// Package targets turns your own match history into the numbers to beat: last hits at each
// checkpoint, and a timing goal for the two core items you actually build.
package targets

import (
	"cmp"
	"math"
	"slices"
	"strconv"
	"sync"
	"time"

	"gourdian/internal/coach"
	"gourdian/internal/dota"
	"gourdian/internal/dotadata"
	"gourdian/internal/model"
)

const incompleteWait = 10 * time.Second

// History is the part of the statistics store the targets are read from.
type History interface {
	HistoryVersion() int64
	RecentOn(heroID int, role string, limit int) ([]model.MatchSummary, error)
	ItemsIn(matchIDs []string) ([]model.ItemTiming, error)
}

// BuildSource is the part of the OpenDota client the item goals are read from.
type BuildSource interface {
	Items() map[string]dotadata.ItemInfo
	BuildFor(heroID int, role string) *dotadata.Build
	ItemTimings(heroID int, item string) ([]dotadata.ItemTiming, bool)
}

// Cache answers the engine, which asks several times a second, so answers are kept until the
// history changes. A partial answer, made while OpenDota was still loading, is retried sooner.
type Cache struct {
	History History
	Builds  BuildSource

	mu      sync.Mutex
	entries map[string]cached
}

type cached struct {
	t        coach.Targets
	history  int64
	complete bool
	at       time.Time
}

func (c *Cache) TargetsFor(heroID int, role string) coach.Targets {
	key := role + "/" + strconv.Itoa(heroID)
	history := c.History.HistoryVersion()
	c.mu.Lock()
	if e, ok := c.entries[key]; ok && e.history == history && (e.complete || time.Since(e.at) < incompleteWait) {
		c.mu.Unlock()
		return e.t
	}
	c.mu.Unlock()

	t, complete := c.build(heroID, role)
	c.mu.Lock()
	if c.entries == nil {
		c.entries = map[string]cached{}
	}
	c.entries[key] = cached{t: t, history: history, complete: complete, at: time.Now()}
	c.mu.Unlock()
	return t
}

// Forget drops every cached answer, so the next ask is built from scratch even though the
// history version hasn't moved.
func (c *Cache) Forget() {
	c.mu.Lock()
	clear(c.entries)
	c.mu.Unlock()
}

func (c *Cache) build(heroID int, role string) (coach.Targets, bool) {
	matches, err := c.History.RecentOn(heroID, role, dota.PersonalGames)
	if err != nil {
		return coach.RoleTargets(role), false
	}
	history := slices.Clone(matches)
	slices.Reverse(history) // newest first
	t := coach.PersonalLastHits(role, history)
	if !dota.Core(role) || c.Builds == nil {
		return t, true
	}
	var complete bool
	t.Items, t.ItemGames, complete = c.itemGoals(heroID, role, history)
	return t, complete
}

// itemGoals picks two core items, the player's usual ones if they have enough history on the
// hero and otherwise the popular build's, and sets a timing goal for each.
func (c *Cache) itemGoals(heroID int, role string, history []model.MatchSummary) ([]coach.ItemGoal, int, bool) {
	items := c.Builds.Items()
	if items == nil {
		return nil, 0, false
	}
	counted := history[:min(len(history), dota.PersonalGames)]
	idList := make([]string, 0, len(counted))
	for _, m := range counted {
		idList = append(idList, m.MatchID)
	}
	rows, _ := c.History.ItemsIn(idList)
	perMatch := map[string]map[string]int{}
	for _, r := range rows {
		if items[r.Item].Cost < dota.GoalItemCost {
			continue
		}
		if perMatch[r.MatchID] == nil {
			perMatch[r.MatchID] = map[string]int{}
		}
		if at, ok := perMatch[r.MatchID][r.Item]; !ok || r.Time < at {
			perMatch[r.MatchID][r.Item] = r.Time
		}
	}
	counts, times := map[string]int{}, map[string][]int{}
	for _, bought := range perMatch {
		names := make([]string, 0, len(bought))
		for n := range bought {
			names = append(names, n)
		}
		slices.SortFunc(names, func(a, b string) int { return bought[a] - bought[b] })
		for _, n := range names[:min(2, len(names))] {
			counts[n]++
		}
		for n, at := range bought {
			times[n] = append(times[n], at)
		}
	}

	var chosen []string
	fromGames := 0
	if len(perMatch) >= 3 {
		for n, count := range counts {
			if count >= 2 {
				chosen = append(chosen, n)
			}
		}
		slices.SortFunc(chosen, func(a, b string) int {
			return cmp.Or(counts[b]-counts[a], dota.Median(times[a])-dota.Median(times[b]))
		})
		if len(chosen) > 0 {
			fromGames = len(perMatch)
		}
	}
	if len(chosen) == 0 {
		build := c.Builds.BuildFor(heroID, role)
		if build == nil || build.Loading {
			return nil, 0, false
		}
		for _, it := range build.Items {
			if (it.Phase == dotadata.PhaseMid || it.Phase == dotadata.PhaseLate) && it.Cost >= dota.GoalItemCost {
				chosen = append(chosen, it.Name)
			}
		}
	}
	chosen = chosen[:min(2, len(chosen))]

	complete := true
	var goals []coach.ItemGoal
	for _, name := range chosen {
		buckets, ok := c.Builds.ItemTimings(heroID, name)
		if !ok {
			complete = false
		}
		good, hasGood := dotadata.GoodTiming(buckets)
		usual := 0
		if len(times[name]) >= 3 {
			usual = dota.Median(times[name])
		}
		by := good
		// Aim a minute under your usual, but never earlier than the timing that wins most.
		if usual > 0 && (!hasGood || usual-60 > good) {
			by = usual - 60
		}
		if by <= 0 {
			continue
		}
		goals = append(goals, coach.ItemGoal{Item: name, Name: items[name].DName, By: roundTo(by, 30), Usual: usual})
	}
	slices.SortFunc(goals, func(a, b coach.ItemGoal) int { return a.By - b.By })
	return goals, fromGames, complete
}

func roundTo(v, step int) int { return int(math.Round(float64(v)/float64(step))) * step }
