package server

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
	"gourdian/internal/stats"
)

const (
	incompleteWait = 10 * time.Second // retry while OpenDota data is still loading
)

// targetCache builds personal targets from the match history and OpenDota's item timings. The
// engine asks several times a second, so answers are kept until the history changes, which
// stats counts for us.
type targetCache struct {
	s       *Server
	mu      sync.Mutex
	entries map[string]cachedTargets
}

type cachedTargets struct {
	t        coach.Targets
	history  int64
	complete bool
	at       time.Time
}

func (tc *targetCache) TargetsFor(heroID int, role string) coach.Targets {
	key := role + "/" + strconv.Itoa(heroID)
	history := tc.s.stats.HistoryVersion()
	tc.mu.Lock()
	if e, ok := tc.entries[key]; ok && e.history == history && (e.complete || time.Since(e.at) < incompleteWait) {
		tc.mu.Unlock()
		return e.t
	}
	tc.mu.Unlock()

	t, complete := tc.build(heroID, role)
	tc.mu.Lock()
	if tc.entries == nil {
		tc.entries = map[string]cachedTargets{}
	}
	tc.entries[key] = cachedTargets{t: t, history: history, complete: complete, at: time.Now()}
	tc.mu.Unlock()
	return t
}

func (tc *targetCache) build(heroID int, role string) (coach.Targets, bool) {
	s := tc.s
	matches, err := s.stats.MatchesWhere(stats.MatchFilter{HeroID: heroID, Role: role, Real: true, Limit: dota.PersonalGames})
	if err != nil {
		return coach.RoleTargets(role), false
	}
	history := slices.Clone(matches)
	slices.Reverse(history) // newest first
	t := coach.PersonalLastHits(role, history)
	if !dota.Core(role) || s.data == nil {
		return t, true
	}
	var complete bool
	t.Items, t.ItemGames, complete = tc.itemGoals(heroID, role, history)
	return t, complete
}

// itemGoals picks two core items, the player's usual ones if they have enough history on the
// hero and otherwise the popular build's, and sets a timing goal for each.
func (tc *targetCache) itemGoals(heroID int, role string, history []stats.MatchSummary) ([]coach.ItemGoal, int, bool) {
	s := tc.s
	items := s.data.Items()
	if items == nil {
		return nil, 0, false
	}
	ids := map[string]bool{}
	var idList []string
	for _, m := range history[:min(len(history), dota.PersonalGames)] {
		ids[m.MatchID] = true
		idList = append(idList, m.MatchID)
	}
	rows, _ := s.stats.ItemsIn(idList)
	perMatch := map[string]map[string]int{}
	for _, r := range rows {
		if !ids[r.MatchID] || items[r.Item].Cost < dota.GoalItemCost {
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
		for n, c := range counts {
			if c >= 2 {
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
		build := s.data.BuildFor(heroID, role)
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
		buckets, ok := s.data.ItemTimings(heroID, name)
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
