package opendota

import (
	"context"
	"fmt"
	"path/filepath"
	"time"
)

// matchupsMaxAge is how long a hero's record is kept; it moves with the patch, not the hour.
const matchupsMaxAge = 3 * 24 * time.Hour

// Matchup is how one hero has fared against another in public games.
type Matchup struct {
	HeroID int `json:"hero_id"`
	Games  int `json:"games_played"`
	Wins   int `json:"wins"`
}

// WinPct is the queried hero's win rate against this one.
func (m Matchup) WinPct() int {
	if m.Games == 0 {
		return 0
	}
	return m.Wins * 100 / m.Games
}

// Matchups is heroID's record against each other hero, nil until it loads; ask about an enemy to
// learn what beats them. A first call only starts the fetch, so the GSI handler never waits.
func (c *Client) Matchups(heroID int) map[int]Matchup {
	if heroID <= 0 {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	m, _, fetch := c.matchups.get(heroID)
	if fetch {
		c.goFetch(func() { c.fetchMatchups(heroID) })
	}
	return m
}

func (c *Client) fetchMatchups(heroID int) {
	ctx, cancel := context.WithTimeout(c.life(), time.Minute)
	defer cancel()
	var rows []Matchup
	err := c.getJSON(ctx, fmt.Sprintf("/heroes/%d/matchups", heroID),
		filepath.Join("matchups", fmt.Sprintf("%d.json", heroID)), matchupsMaxAge, &rows)
	c.mu.Lock()
	defer c.mu.Unlock()
	if err != nil || len(rows) == 0 {
		c.log.Warn("no matchups for hero; will retry", "hero_id", heroID, "err", err)
		c.matchups.fail(heroID)
		return
	}
	against := make(map[int]Matchup, len(rows))
	for _, r := range rows {
		against[r.HeroID] = r
	}
	c.matchups.done(heroID, against)
	c.log.Info("matchups loaded", "hero_id", heroID, "against", len(against))
}
