package opendota

import (
	"context"
	"fmt"
	"path/filepath"
	"time"
)

// matchupsMaxAge is how long a hero's record against the rest is kept. It moves with the
// patch, not the hour.
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

// Matchups is how a hero has fared against every other, by the other hero's id, or nil until
// it loads. The answer is from the queried hero's point of view, so to learn what beats an
// enemy you ask about the enemy: one call covers every hero the player might pick, instead of
// one call for each.
//
// The first call starts the fetch and returns nothing, like the builds do, so the game-state
// handler never waits on OpenDota.
func (c *Client) Matchups(heroID int) map[int]Matchup {
	if heroID <= 0 {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if m, ok := c.matchups[heroID]; ok {
		return m
	}
	if !c.matchupsPending[heroID] && time.Since(c.matchupsFailed[heroID]) >= buildRetry {
		c.matchupsPending[heroID] = true
		go c.fetchMatchups(heroID)
	}
	return nil
}

func (c *Client) fetchMatchups(heroID int) {
	ctx, cancel := context.WithTimeout(c.life(), time.Minute)
	defer cancel()
	var rows []Matchup
	err := c.getJSON(ctx, fmt.Sprintf("/heroes/%d/matchups", heroID),
		filepath.Join("matchups", fmt.Sprintf("%d.json", heroID)), matchupsMaxAge, &rows)
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.matchupsPending, heroID)
	if err != nil || len(rows) == 0 {
		c.log.Warn("no matchups for hero; will retry", "hero_id", heroID, "err", err)
		c.matchupsFailed[heroID] = time.Now()
		return
	}
	against := make(map[int]Matchup, len(rows))
	for _, r := range rows {
		against[r.HeroID] = r
	}
	c.matchups[heroID] = against
	c.log.Info("matchups loaded", "hero_id", heroID, "against", len(against))
}
