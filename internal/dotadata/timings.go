package dotadata

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
	"strconv"
	"time"
)

const timingsMaxAge = 72 * time.Hour

// ItemTiming is one bucket of OpenDota's item timing scenarios: games where the item was
// bought by Time (game clock seconds), and how many of them were won.
type ItemTiming struct {
	Time  int
	Games int
	Wins  int
}

type timingsKey struct {
	hero int
	item string
}

// ItemTimings returns the timing buckets for a hero's item, or false while they're fetched.
func (c *Client) ItemTimings(heroID int, item string) ([]ItemTiming, bool) {
	key := timingsKey{heroID, item}
	c.mu.Lock()
	defer c.mu.Unlock()
	if t, ok := c.timings[key]; ok {
		return t, true
	}
	if c.timingsPending[key] || time.Since(c.timingsFailed[key]) < buildRetry {
		return nil, false
	}
	c.timingsPending[key] = true
	go c.fetchTimings(key)
	return nil, false
}

func (c *Client) fetchTimings(key timingsKey) {
	ctx, cancel := context.WithTimeout(c.life(), time.Minute)
	defer cancel()
	var raw []struct {
		Time  int         `json:"time"`
		Games json.Number `json:"games"`
		Wins  json.Number `json:"wins"`
	}
	path := fmt.Sprintf("/scenarios/itemTimings?item=%s&hero_id=%d", key.item, key.hero)
	err := c.getJSON(ctx, path, filepath.Join("timings", fmt.Sprintf("%d_%s.json", key.hero, key.item)), timingsMaxAge, &raw)
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.timingsPending, key)
	if err != nil {
		c.log.Warn("no item timings; will retry", "hero_id", key.hero, "item", key.item, "err", err)
		c.timingsFailed[key] = time.Now()
		return
	}
	out := make([]ItemTiming, 0, len(raw))
	for _, r := range raw {
		games, _ := strconv.Atoi(r.Games.String())
		wins, _ := strconv.Atoi(r.Wins.String())
		out = append(out, ItemTiming{Time: r.Time, Games: games, Wins: wins})
	}
	slices.SortFunc(out, func(a, b ItemTiming) int { return a.Time - b.Time })
	c.timings[key] = out
}

// GoodTiming picks a timing worth aiming for: the earliest bucket holding at least a tenth of
// the games that wins at least as often as the item does overall. Without one, it's the
// median timing. It needs enough games to mean something.
func GoodTiming(buckets []ItemTiming) (int, bool) {
	var games, wins int
	for _, b := range buckets {
		games, wins = games+b.Games, wins+b.Wins
	}
	if games < 30 {
		return 0, false
	}
	overall := float64(wins) / float64(games)
	for _, b := range buckets {
		if b.Games*10 >= games && float64(b.Wins)/float64(b.Games) >= overall {
			return b.Time, true
		}
	}
	seen := 0
	for _, b := range buckets {
		if seen += b.Games; seen*2 >= games {
			return b.Time, true
		}
	}
	return 0, false
}

// CoreItemTimes keeps the timings of finished items costing at least minCost, dropping
// components that were combined into a bigger item bought at the same time or later.
func CoreItemTimes(times map[string]int, items map[string]ItemInfo, minCost int) map[string]int {
	upgraded := map[string]bool{}
	for name, t := range times {
		for _, part := range items[name].Components {
			if bought, ok := times[part]; ok && bought <= t {
				upgraded[part] = true
			}
		}
	}
	out := map[string]int{}
	for name, t := range times {
		if info, ok := items[name]; ok && info.Cost >= minCost && info.Tier == 0 && !upgraded[name] {
			out[name] = t
		}
	}
	return out
}
