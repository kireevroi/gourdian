package dotadata

import (
	"cmp"
	"math"
	"slices"
	"strconv"
	"strings"
)

type ItemInfo struct {
	ID         int      `json:"id"`
	DName      string   `json:"dname"`
	Cost       int      `json:"cost"`
	Components []string `json:"components"`
	Qual       string   `json:"qual"`
	Tier       int      `json:"tier"`
	Img        string   `json:"img"`
	Created    bool     `json:"created"` // made from a recipe, not bought as it is
}

// standalone are uncrafted items pros buy for their own use, not as parts.
var standalone = map[string]bool{"blink": true, "gem": true}

// isPart is an item bought only to become part of another, such as an Ultimate Orb.
func isPart(name string, info ItemInfo) bool {
	return !info.Created && !standalone[name] && (info.Qual == "secret_shop" || info.Qual == "component")
}

type HeroInfo struct {
	ID            int      `json:"id"`
	Name          string   `json:"name"`
	LocalizedName string   `json:"localized_name"`
	PrimaryAttr   string   `json:"primary_attr"`
	Roles         []string `json:"roles"`
	Img           string   `json:"img"`
}

// Build phases, in purchase order.
const (
	PhaseStart = "start"
	PhaseEarly = "early"
	PhaseMid   = "mid"
	PhaseLate  = "late"
)

type BuildItem struct {
	Name  string `json:"name"`
	DName string `json:"dname"`
	Cost  int    `json:"cost"`
	Phase string `json:"phase"`
	Img   string `json:"img,omitempty"`
}

type Build struct {
	HeroID int         `json:"hero_id"`
	Items  []BuildItem `json:"items"`
	// Position is 1 to 5 for a build from pro games in that position, 0 for every position.
	Position int `json:"position,omitempty"`
	Games    int `json:"games,omitempty"`
	// Won is set for builds from won pro games only. Without it the build is OpenDota's
	// itemPopularity: up to 100 recent pro games of the hero, won or lost, in every position.
	Won bool `json:"won,omitempty"`
	// Loading is set while the position's own build may still replace this one.
	Loading bool `json:"-"`
}

// Popularity is OpenDota's itemPopularity response: phase key -> item id -> purchase count.
type Popularity map[string]map[string]int

var phaseSpecs = []struct {
	phase, key string
	limit      int
	minCost    int
	consumable bool
}{
	{PhaseStart, "start_game_items", 6, 0, true},
	{PhaseEarly, "early_game_items", 5, 0, false},
	{PhaseMid, "mid_game_items", 6, 1000, false},
	{PhaseLate, "late_game_items", 6, 2000, false},
}

func BuildFromPopularity(heroID int, pop Popularity, items map[string]ItemInfo) *Build {
	byID := make(map[int]string, len(items))
	for name, it := range items {
		byID[it.ID] = name
	}
	type cand struct {
		name  string
		count int
	}
	seen := map[string]bool{}
	var phases [][]BuildItem
	for _, spec := range phaseSpecs {
		var cands []cand
		for idStr, count := range pop[spec.key] {
			id, err := strconv.Atoi(idStr)
			if err != nil {
				continue
			}
			name, ok := byID[id]
			if !ok {
				continue
			}
			info := items[name]
			switch {
			case strings.HasPrefix(name, "recipe_"), info.Tier > 0, info.DName == "":
				continue
			case !spec.consumable && strings.Contains(info.Qual, "consumable"):
				continue
			case info.Cost < spec.minCost:
				continue
			case spec.minCost > 0 && isPart(name, info):
				continue
			}
			cands = append(cands, cand{name, count})
		}
		slices.SortFunc(cands, func(a, b cand) int {
			return cmp.Or(cmp.Compare(b.count, a.count), cmp.Compare(a.name, b.name))
		})
		var chosen []BuildItem
		for _, c := range cands {
			if len(chosen) == spec.limit || c.count*100 < cands[0].count*15 {
				break
			}
			if seen[c.name] {
				continue
			}
			seen[c.name] = true
			info := items[c.name]
			chosen = append(chosen, BuildItem{Name: c.name, DName: info.DName, Cost: info.Cost, Phase: spec.phase, Img: info.Img})
		}
		slices.SortStableFunc(chosen, func(a, b BuildItem) int { return cmp.Compare(a.Cost, b.Cost) })
		phases = append(phases, chosen)
	}

	b := &Build{HeroID: heroID}
	for i, phase := range phases {
		for _, it := range phase {
			if i == 0 || !componentOfLater(it.Name, phases[i:], items) {
				b.Items = append(b.Items, it)
			}
		}
	}
	return b
}

func componentOfLater(name string, phases [][]BuildItem, items map[string]ItemInfo) bool {
	for _, phase := range phases {
		for _, other := range phase {
			if other.Name != name && Contains(other.Name, name, items) {
				return true
			}
		}
	}
	return false
}

func Contains(parent, component string, items map[string]ItemInfo) bool {
	for _, c := range items[parent].Components {
		if c == component || Contains(c, component, items) {
			return true
		}
	}
	return false
}

func OwnedClosure(held []string, items map[string]ItemInfo) map[string]bool {
	out := map[string]bool{}
	var add func(string)
	add = func(n string) {
		if out[n] {
			return
		}
		out[n] = true
		for _, c := range items[n].Components {
			add(c)
		}
	}
	for _, h := range held {
		add(h)
	}
	return out
}

func RemainingCost(target string, held []string, items map[string]ItemInfo) int {
	counts := map[string]int{}
	for _, h := range held {
		counts[h]++
	}
	var need func(name string, top bool) int
	need = func(name string, top bool) int {
		if !top && counts[name] > 0 {
			counts[name]--
			return 0
		}
		info := items[name]
		if len(info.Components) == 0 {
			return info.Cost
		}
		recipe := info.Cost
		for _, c := range info.Components {
			recipe -= items[c].Cost
		}
		total := max(recipe, 0)
		for _, c := range info.Components {
			total += need(c, false)
		}
		return total
	}
	return need(target, true)
}

type Progress struct {
	Owned   []bool
	Skipped []bool
	Next    int
}

// Progress marks items the player built past, or whose phase is long over, as skipped so
// an item they chose not to buy doesn't block every later suggestion.
func (b *Build) Progress(held []string, items map[string]ItemInfo, clock int) Progress {
	p := Progress{Owned: make([]bool, len(b.Items)), Skipped: make([]bool, len(b.Items)), Next: -1}
	owned := OwnedClosure(held, items)
	lastOwned := -1
	for i, it := range b.Items {
		if p.Owned[i] = owned[it.Name]; p.Owned[i] {
			lastOwned = i
		}
	}
	for i, it := range b.Items {
		if p.Owned[i] {
			continue
		}
		p.Skipped[i] = i < lastOwned || clock >= phaseEnds[it.Phase]
		if !p.Skipped[i] && p.Next < 0 {
			p.Next = i
		}
	}
	return p
}

func (b *Build) Next(held []string, items map[string]ItemInfo, clock int) (BuildItem, bool) {
	if b == nil {
		return BuildItem{}, false
	}
	if p := b.Progress(held, items, clock); p.Next >= 0 {
		return b.Items[p.Next], true
	}
	return BuildItem{}, false
}

var phaseEnds = map[string]int{PhaseStart: 0, PhaseEarly: 900, PhaseMid: 1800, PhaseLate: math.MaxInt}
