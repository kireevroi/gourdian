package opendota

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"strconv"
	"time"
)

// OpenDota's itemPopularity mixes every position a hero is played in, won or lost, so a support
// Pudge's wards end up in a mid Pudge's build. Its SQL explorer over parsed pro matches can split
// them and keep only the games the hero's team won.
const (
	// MinProGames is the fewest pro games a build is made from; below it the trainer falls back.
	MinProGames = 12
	// ProDays is how far back the pro games behind builds and skill orders go.
	ProDays = 120
)

// positionKey is one hero in one position, as the builds and skill orders are looked up.
type positionKey struct {
	hero, pos int
	won       bool // only games the hero's team won
}

// filter is the SQL condition that keeps the key's games.
func (k positionKey) filter() string {
	if k.won {
		return "won"
	}
	return "true"
}

func (k positionKey) file(kind string) string {
	games := "all"
	if k.won {
		games = "won"
	}
	return fmt.Sprintf("%d-pos%d%s-%s.json", k.hero, k.pos, kind, games)
}

type positionData struct {
	Games int        `json:"games"`
	Pop   Popularity `json:"popularity"`
	// At is when each item in Pop was bought, and is what tells a build step from a part of one.
	At BuyTimes `json:"at"`
}

// positionSQL: explorer rows don't say the position, so the three richest players on a team
// are its cores, told apart by lane, and the richer support is the 4. Position 0 is every position.
func positionSQL(key positionKey) string {
	hero, pos := key.hero, key.pos
	return fmt.Sprintf(`WITH p AS (
  SELECT pm.match_id, pm.hero_id, pm.lane_role, pm.purchase_log, (pm.player_slot < 128) = m.radiant_win AS won,
    ROW_NUMBER() OVER (PARTITION BY pm.match_id, pm.player_slot < 128 ORDER BY pm.gold_per_min DESC) AS farm
  FROM player_matches pm JOIN matches m USING (match_id)
  WHERE m.start_time > extract(epoch from now() - interval '%d days')
    AND pm.match_id IN (SELECT match_id FROM player_matches WHERE hero_id = %d)
), q AS (
  SELECT match_id, purchase_log FROM p WHERE hero_id = %d AND %s AND purchase_log IS NOT NULL AND (%d = 0 OR CASE
    WHEN farm <= 3 AND lane_role = 2 THEN 2 WHEN farm <= 3 AND lane_role = 1 THEN 1
    WHEN farm <= 3 AND lane_role = 3 THEN 3 WHEN farm <= 3 THEN 0 WHEN farm = 4 THEN 4 ELSE 5 END = %d)
)
SELECT CASE WHEN (e->>'time')::int <= 0 THEN 'start_game_items' WHEN (e->>'time')::int < 600 THEN 'early_game_items'
    WHEN (e->>'time')::int < 1500 THEN 'mid_game_items' ELSE 'late_game_items' END AS phase,
  e->>'key' AS item, count(DISTINCT match_id)::int AS games, (SELECT count(*) FROM q)::int AS total,
  percentile_disc(0.5) WITHIN GROUP (ORDER BY (e->>'time')::int)::int AS at
FROM q, unnest(purchase_log) e GROUP BY 1, 2`, ProDays, hero, hero, key.filter(), pos, pos)
}

// proBuild is the hero's build from pro games in one position (0 for every position), won
// only or all of them: fallback while it loads and when there are too few such games.
func (c *Client) proBuild(heroID, pos int, won bool, fallback *Build) *Build {
	if heroID <= 0 {
		return fallback
	}
	key := positionKey{heroID, pos, won}
	c.mu.Lock()
	defer c.mu.Unlock()
	b, ok, fetch := c.posBuilds.get(key)
	if ok {
		if b != nil {
			return b
		}
		return fallback
	}
	if fetch {
		go c.fetchPositionBuild(key)
	}
	if fallback == nil {
		return nil
	}
	stand := *fallback
	stand.Loading = true
	return &stand
}

func (c *Client) fetchPositionBuild(key positionKey) {
	<-c.ready
	ctx, cancel := context.WithTimeout(c.life(), time.Minute)
	defer cancel()
	data, err := c.loadPositionData(ctx, key)
	c.mu.Lock()
	defer c.mu.Unlock()
	if err != nil || c.items == nil {
		c.log.Warn("no pro build; will retry", "hero_id", key.hero, "position", key.pos, "won_only", key.won, "err", err)
		c.posBuilds.fail(key)
		return
	}
	var b *Build
	if data.Games >= MinProGames {
		b = BuildFromPopularity(key.hero, data.Pop, data.At, c.items)
		b.Position, b.Games, b.Won = key.pos, data.Games, key.won
	}
	c.posBuilds.done(key, b)
	c.log.Info("pro build loaded", "hero_id", key.hero, "position", key.pos, "won_only", key.won, "pro_games", data.Games, "used", b != nil)
}

func (c *Client) loadPositionData(ctx context.Context, key positionKey) (positionData, error) {
	var out positionData
	err := c.cachedBytes(filepath.Join("builds", key.file("")), buildMaxAge, func() ([]byte, error) {
		var fresh positionData
		raw, err := c.fetch(ctx, "/explorer?sql="+url.QueryEscape(positionSQL(key)))
		var resp struct {
			Rows []struct {
				Phase string `json:"phase"`
				Item  string `json:"item"`
				Games int    `json:"games"`
				Total int    `json:"total"`
				At    int    `json:"at"`
			} `json:"rows"`
			Err any `json:"err"`
		}
		if err == nil {
			err = json.Unmarshal(raw, &resp)
		}
		if err == nil && resp.Err != nil {
			err = fmt.Errorf("explorer: %v", resp.Err)
		}
		if err != nil {
			return nil, err
		}
		items := c.Items()
		fresh.Pop, fresh.At = Popularity{}, BuyTimes{}
		for _, r := range resp.Rows {
			fresh.Games = r.Total
			info, ok := items[r.Item]
			if !ok {
				continue
			}
			if fresh.Pop[r.Phase] == nil {
				fresh.Pop[r.Phase], fresh.At[r.Phase] = map[string]int{}, map[string]int{}
			}
			id := strconv.Itoa(info.ID)
			fresh.Pop[r.Phase][id], fresh.At[r.Phase][id] = r.Games, r.At
		}
		return json.Marshal(fresh)
	}, func(data []byte) error {
		var fresh positionData
		if err := json.Unmarshal(data, &fresh); err != nil {
			return err
		}
		if fresh.Games > 0 && fresh.At == nil {
			return errors.New("cached before purchase times were kept")
		}
		out = fresh
		return nil
	}, nil)
	return out, err
}
