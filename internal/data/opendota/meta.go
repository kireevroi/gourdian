package opendota

import (
	"context"
	"time"
)

const (
	// metaMaxAge is how long the hero meta is kept: it moves with the patch, not the minute.
	metaMaxAge = 24 * time.Hour
	// metaMinGames is the fewest games a bracket needs before its win rate is read on its own.
	metaMinGames = 200
)

// HeroMeta is how a hero is doing in public games right now. OpenDota counts picks and wins by
// rank bracket, 1 (Herald) to 8 (Immortal), so pick advice can follow the player's own bracket
// instead of everybody's average.
type HeroMeta struct {
	ID          int
	Roles       []string
	AttackType  string
	PrimaryAttr string
	// Pick and Win are games and wins at each bracket; index 0 holds every bracket together.
	Pick [9]int
	Win  [9]int
}

// WinPct is the hero's win rate at the bracket and the games behind it. A bracket too thin to
// read falls back to every bracket together, so a rare hero still says something. Games of 0
// means the hero has no record worth using.
func (m HeroMeta) WinPct(bracket int) (pct, games int) {
	if bracket >= 1 && bracket < len(m.Pick) && m.Pick[bracket] >= metaMinGames {
		return m.Win[bracket] * 100 / m.Pick[bracket], m.Pick[bracket]
	}
	if m.Pick[0] == 0 {
		return 0, 0
	}
	return m.Win[0] * 100 / m.Pick[0], m.Pick[0]
}

// Melee reports whether the hero fights in melee, for reading a team's shape.
func (m HeroMeta) Melee() bool { return m.AttackType == "Melee" }

// heroStatsRow is one hero in OpenDota's /heroStats. The bracket counts are named by number,
// so they are spelled out here the way gsi.Hero spells out its talents.
type heroStatsRow struct {
	ID          int      `json:"id"`
	Roles       []string `json:"roles"`
	AttackType  string   `json:"attack_type"`
	PrimaryAttr string   `json:"primary_attr"`

	Pick1 int `json:"1_pick"`
	Win1  int `json:"1_win"`
	Pick2 int `json:"2_pick"`
	Win2  int `json:"2_win"`
	Pick3 int `json:"3_pick"`
	Win3  int `json:"3_win"`
	Pick4 int `json:"4_pick"`
	Win4  int `json:"4_win"`
	Pick5 int `json:"5_pick"`
	Win5  int `json:"5_win"`
	Pick6 int `json:"6_pick"`
	Win6  int `json:"6_win"`
	Pick7 int `json:"7_pick"`
	Win7  int `json:"7_win"`
	Pick8 int `json:"8_pick"`
	Win8  int `json:"8_win"`
}

func (r heroStatsRow) meta() HeroMeta {
	m := HeroMeta{ID: r.ID, Roles: r.Roles, AttackType: r.AttackType, PrimaryAttr: r.PrimaryAttr}
	m.Pick = [9]int{0, r.Pick1, r.Pick2, r.Pick3, r.Pick4, r.Pick5, r.Pick6, r.Pick7, r.Pick8}
	m.Win = [9]int{0, r.Win1, r.Win2, r.Win3, r.Win4, r.Win5, r.Win6, r.Win7, r.Win8}
	for i := 1; i < len(m.Pick); i++ {
		m.Pick[0] += m.Pick[i]
		m.Win[0] += m.Win[i]
	}
	return m
}

// Meta is how every hero is doing in public games, by rank bracket, or nil until it loads.
// The first call starts the fetch; it never blocks, so the GSI handler can ask on any update.
func (c *Client) Meta() map[int]HeroMeta {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.meta != nil || c.metaPending || time.Since(c.metaFailed) < buildRetry {
		return c.meta
	}
	c.metaPending = true
	go c.fetchMeta()
	return nil
}

func (c *Client) fetchMeta() {
	ctx, cancel := context.WithTimeout(c.life(), time.Minute)
	defer cancel()
	var rows []heroStatsRow
	err := c.getJSON(ctx, "/heroStats", "herostats.json", metaMaxAge, &rows)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.metaPending = false
	if err != nil || len(rows) == 0 {
		c.log.Warn("no hero meta; will retry", "err", err)
		c.metaFailed = time.Now()
		return
	}
	meta := make(map[int]HeroMeta, len(rows))
	for _, r := range rows {
		meta[r.ID] = r.meta()
	}
	c.meta = meta
	c.log.Info("hero meta loaded", "heroes", len(meta))
}
