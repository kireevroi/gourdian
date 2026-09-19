package dotadata

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
)

// SkillBuild is the order professional players level the hero's abilities in one position,
// one ability per skill point. Talents and innate abilities aren't in it.
type SkillBuild struct {
	HeroID   int      `json:"hero_id"`
	Position int      `json:"position,omitempty"` // 0 when it comes from every position
	Games    int      `json:"games"`
	Won      bool     `json:"won,omitempty"` // from won games only
	Order    []string `json:"order"`
}

type abilityInfo struct {
	DName    string `json:"dname"`
	IsInnate bool   `json:"is_innate"`
}

// SkillBuildFor is nil while it loads, and has no order when too few pro games have the hero.
func (c *Client) SkillBuildFor(heroID int, role string) *SkillBuild {
	if heroID <= 0 {
		return nil
	}
	key := positionKey{heroID, Positions[role], true}
	c.mu.Lock()
	defer c.mu.Unlock()
	if b, ok := c.skillBuilds[key]; ok {
		return b
	}
	if !c.skillPending[key] && time.Since(c.skillFailed[key]) >= buildRetry {
		c.skillPending[key] = true
		go c.fetchSkillBuild(key)
	}
	return nil
}

// AbilityName is how the game writes an ability, such as "Ball Lightning".
func (c *Client) AbilityName(name string) string {
	c.mu.RLock()
	info, ok := c.abilities[name]
	c.mu.RUnlock()
	if ok && info.DName != "" {
		return info.DName
	}
	return name
}

func (c *Client) fetchSkillBuild(key positionKey) {
	<-c.ready
	ctx, cancel := context.WithTimeout(c.life(), time.Minute)
	defer cancel()
	// Won games in the position, then all games there, then the same in every position.
	tries := []positionKey{key, {key.hero, key.pos, false}}
	if key.pos != 0 {
		tries = append(tries, positionKey{key.hero, 0, true}, positionKey{key.hero, 0, false})
	}
	var b *SkillBuild
	var err error
	for _, k := range tries {
		if b, err = c.loadSkillBuild(ctx, k); err != nil || b != nil {
			break
		}
	}
	if err == nil && b == nil {
		b = &SkillBuild{HeroID: key.hero}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.skillPending, key)
	if err != nil {
		c.log.Warn("no skill build for hero; will retry", "hero_id", key.hero, "position", key.pos, "err", err)
		c.skillFailed[key] = time.Now()
		return
	}
	c.skillBuilds[key] = b
	c.log.Info("skill build loaded", "hero_id", key.hero, "position", b.Position, "won_only", b.Won, "pro_games", b.Games, "points", len(b.Order))
}

// loadSkillBuild is nil, without an error, when there are too few pro games to trust.
func (c *Client) loadSkillBuild(ctx context.Context, key positionKey) (*SkillBuild, error) {
	if err := c.loadAbilities(ctx); err != nil {
		return nil, err
	}
	hero, ok := c.Hero(key.hero)
	if !ok {
		return nil, fmt.Errorf("unknown hero %d", key.hero)
	}
	var seqs [][]int
	cachePath := filepath.Join(c.cacheDir, "builds", key.file("-skills"))
	if fi, err := os.Stat(cachePath); err == nil && time.Since(fi.ModTime()) < buildMaxAge {
		if raw, err := os.ReadFile(cachePath); err == nil && json.Unmarshal(raw, &seqs) == nil {
			return c.skillBuildFrom(key, hero.Name, seqs), nil
		}
	}
	raw, err := c.fetch(ctx, "/explorer?sql="+url.QueryEscape(skillSQL(key)))
	var resp struct {
		Rows []struct {
			Order []int `json:"ability_upgrades_arr"`
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
		if raw, readErr := os.ReadFile(cachePath); readErr == nil && json.Unmarshal(raw, &seqs) == nil {
			return c.skillBuildFrom(key, hero.Name, seqs), nil
		}
		return nil, err
	}
	for _, r := range resp.Rows {
		seqs = append(seqs, r.Order)
	}
	if saved, err := json.Marshal(seqs); err == nil && os.MkdirAll(filepath.Dir(cachePath), 0o755) == nil {
		_ = os.WriteFile(cachePath, saved, 0o644)
	}
	return c.skillBuildFrom(key, hero.Name, seqs), nil
}

func (c *Client) skillBuildFrom(key positionKey, heroName string, seqs [][]int) *SkillBuild {
	c.mu.RLock()
	own := map[string]bool{}
	for _, a := range c.heroAbilities[heroName] {
		if a != "generic_hidden" && !c.abilities[a].IsInnate {
			own[a] = true
		}
	}
	var named [][]string
	for _, seq := range seqs {
		var names []string
		for _, id := range seq {
			if name := c.abilityIDs[strconv.Itoa(id)]; own[name] {
				names = append(names, name)
			}
		}
		if len(names) > 0 {
			named = append(named, names)
		}
	}
	c.mu.RUnlock()
	if len(named) < positionMinGames {
		return nil
	}
	return &SkillBuild{HeroID: key.hero, Position: key.pos, Games: len(named), Won: key.won, Order: SkillConsensus(named)}
}

// SkillConsensus is the skill order most pros agree on: at each point the ability taken most
// often there, unless it's already maxed or pros never take that level of it so early.
func SkillConsensus(seqs [][]string) []string {
	maxLevel := map[string]int{}
	earliest := map[string]int{} // "ability#level" -> the first point any pro took it at
	steps := 0
	for _, seq := range seqs {
		steps = max(steps, len(seq))
		have := map[string]int{}
		for i, a := range seq {
			have[a]++
			maxLevel[a] = max(maxLevel[a], have[a])
			k := a + "#" + strconv.Itoa(have[a])
			if at, ok := earliest[k]; !ok || i < at {
				earliest[k] = i
			}
		}
	}
	var order []string
	have := map[string]int{}
	for i := range steps {
		votes := map[string]int{}
		for _, seq := range seqs {
			if i < len(seq) {
				votes[seq[i]]++
			}
		}
		var cands []string
		for a := range maxLevel {
			at, ok := earliest[a+"#"+strconv.Itoa(have[a]+1)]
			if have[a] < maxLevel[a] && ok && at <= i {
				cands = append(cands, a)
			}
		}
		if len(cands) == 0 {
			break
		}
		slices.SortFunc(cands, func(a, b string) int { return cmp.Or(votes[b]-votes[a], strings.Compare(a, b)) })
		order = append(order, cands[0])
		have[cands[0]]++
	}
	return order
}

func (c *Client) loadAbilities(ctx context.Context) error {
	c.mu.RLock()
	loaded := c.abilities != nil
	c.mu.RUnlock()
	if loaded {
		return nil
	}
	var ids map[string]string
	var heroes map[string]struct {
		Abilities []json.RawMessage `json:"abilities"`
	}
	var info map[string]abilityInfo
	for _, get := range []struct {
		path, file string
		out        any
	}{{"/constants/ability_ids", "ability_ids.json", &ids}, {"/constants/hero_abilities", "hero_abilities.json", &heroes}, {"/constants/abilities", "abilities.json", &info}} {
		if err := c.getJSON(ctx, get.path, get.file, constantsMaxAge, get.out); err != nil {
			return err
		}
	}
	byHero := make(map[string][]string, len(heroes))
	for name, h := range heroes {
		for _, raw := range h.Abilities {
			var a string
			if json.Unmarshal(raw, &a) == nil {
				byHero[name] = append(byHero[name], a)
			}
		}
	}
	c.mu.Lock()
	c.abilityIDs, c.heroAbilities, c.abilities = ids, byHero, info
	c.mu.Unlock()
	return nil
}

// skillSQL finds the hero's skill orders in pro games, in one position or in all of them.
func skillSQL(key positionKey) string {
	hero, pos := key.hero, key.pos
	return fmt.Sprintf(`WITH p AS (
  SELECT pm.match_id, pm.hero_id, pm.lane_role, pm.ability_upgrades_arr, m.start_time, (pm.player_slot < 128) = m.radiant_win AS won,
    ROW_NUMBER() OVER (PARTITION BY pm.match_id, pm.player_slot < 128 ORDER BY pm.gold_per_min DESC) AS farm
  FROM player_matches pm JOIN matches m USING (match_id)
  WHERE m.start_time > extract(epoch from now() - interval '%d days')
    AND pm.match_id IN (SELECT match_id FROM player_matches WHERE hero_id = %d)
)
SELECT ability_upgrades_arr FROM p WHERE hero_id = %d AND %s AND ability_upgrades_arr IS NOT NULL AND (%d = 0 OR CASE
    WHEN farm <= 3 AND lane_role = 2 THEN 2 WHEN farm <= 3 AND lane_role = 1 THEN 1
    WHEN farm <= 3 AND lane_role = 3 THEN 3 WHEN farm <= 3 THEN 0 WHEN farm = 4 THEN 4 ELSE 5 END = %d)
ORDER BY start_time DESC LIMIT 300`, positionDays, hero, hero, key.filter(), pos, pos)
}
