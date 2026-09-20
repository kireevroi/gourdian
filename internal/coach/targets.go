package coach

import (
	"math"
	"slices"

	"gourdian/internal/dota"
	"gourdian/internal/dotadata"
	"gourdian/internal/model"
)

var paceCheckpoints = []int{300, 600, 900, 1200, 1800}

// paceTargets are last hits at each checkpoint for players without enough games on a hero.
var paceTargets = map[string][]int{
	dota.Carry:   {30, 65, 110, 160, 270},
	dota.Mid:     {30, 60, 100, 145, 240},
	dota.Offlane: {18, 40, 65, 95, 160},
}

// Targets are one hero and position's goals: last hits at each checkpoint and core item timings.
type Targets struct {
	LastHits []int      `json:"last_hits,omitempty"` // per checkpoint; none for supports
	Usual    []int      `json:"usual,omitempty"`     // the player's median per checkpoint, 0 when unknown
	Games    int        `json:"games"`               // matches the personal numbers come from
	Items    []ItemGoal `json:"items,omitempty"`
	// ItemGames is how many of the player's games the items were picked from; 0 means the pro build.
	ItemGames int `json:"item_games,omitempty"`
}

type ItemGoal struct {
	Item  string `json:"item"`
	Name  string `json:"name"`
	By    int    `json:"by"`              // game clock seconds
	Usual int    `json:"usual,omitempty"` // the player's median timing, 0 when unknown
}

// TargetSource supplies personal targets; the trainer builds them from match history.
type TargetSource interface {
	TargetsFor(heroID int, role string) Targets
}

// RoleTargets are the targets without any history.
func RoleTargets(role string) Targets { return Targets{LastHits: slices.Clone(paceTargets[role])} }

const (
	personalMinimum = 3
	// stretchPercent is how far above the player's usual their targets go.
	stretchPercent  = 10
	personalStretch = 1 + stretchPercent/100.0
)

// PersonalLastHits sets each checkpoint's target 10% above the player's median over their
// last 10 matches on the hero and position (newest first), where they have at least 3.
func PersonalLastHits(role string, history []model.MatchSummary) Targets {
	t := RoleTargets(role)
	if t.LastHits == nil {
		return t
	}
	t.Usual = make([]int, len(paceCheckpoints))
	for i, cp := range paceCheckpoints {
		var values []int
		for _, m := range history[:min(len(history), dota.PersonalGames)] {
			if lh, ok := m.LastHitsAt[dota.Clock(cp)]; ok {
				values = append(values, lh)
			}
		}
		if len(values) < personalMinimum {
			continue
		}
		t.Games = max(t.Games, len(values))
		t.Usual[i] = dota.Median(values)
		t.LastHits[i] = int(math.Round(float64(t.Usual[i]) * personalStretch))
	}
	for i := 1; i < len(t.LastHits); i++ {
		t.LastHits[i] = max(t.LastHits[i], t.LastHits[i-1])
	}
	return t
}

// LastHitMap labels targets by checkpoint, like "10:00", for review prompts.
func (t Targets) LastHitMap() map[string]int {
	out := map[string]int{}
	for i, v := range t.LastHits {
		out[dota.Clock(paceCheckpoints[i])] = v
	}
	return out
}

func expectedLastHits(targets []int, clock int) (int, bool) {
	if len(targets) == 0 || clock <= 0 {
		return 0, len(targets) > 0
	}
	prevT, prevV := 0, 0
	for i, cp := range paceCheckpoints {
		if clock <= cp {
			return prevV + (targets[i]-prevV)*(clock-prevT)/(cp-prevT), true
		}
		prevT, prevV = cp, targets[i]
	}
	last := len(paceCheckpoints) - 1
	perSec := float64(targets[last]-targets[last-1]) / float64(paceCheckpoints[last]-paceCheckpoints[last-1])
	return targets[last] + int(perSec*float64(clock-paceCheckpoints[last])), true
}

// switchedFrom reports whether the player finished another item worth at least 80% of the
// goal instead, which means they changed the build rather than fell behind on it.
func (c *Ctx) switchedFrom(g ItemGoal, items map[string]dotadata.ItemInfo) bool {
	goalCost := items[g.Item].Cost
	for name, at := range c.m.itemSeen {
		cost := items[name].Cost
		if at >= 0 && name != g.Item && cost >= dota.GoalItemCost && cost*5 >= goalCost*4 &&
			!dotadata.Contains(g.Item, name, items) && !dotadata.Contains(name, g.Item, items) {
			return true
		}
	}
	return false
}

// seeItems records when each item first appears. Items already held when the trainer first
// sees a match that's under way get -1, since their purchase time is unknown.
func (m *match) seeItems(held []string, clock int, firstSeen bool) {
	if m.itemSeen == nil {
		m.itemSeen = map[string]int{}
	}
	for _, n := range held {
		if _, ok := m.itemSeen[n]; ok {
			continue
		}
		if firstSeen && clock > 60 {
			m.itemSeen[n] = -1
		} else {
			m.itemSeen[n] = clock
		}
	}
}
