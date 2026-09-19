package dotadata

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"slices"
	"strconv"
	"time"
)

// Match is the part of OpenDota's /matches/{id} response the trainer uses. Fields only a
// replay parse provides (lh_t, first_purchase_time, benchmarks, …) are empty until parsed.
type Match struct {
	MatchID    int64 `json:"match_id"`
	Duration   int   `json:"duration"`
	StartTime  int64 `json:"start_time"`
	RadiantWin bool  `json:"radiant_win"`
	LobbyType  int   `json:"lobby_type"`
	GameMode   int   `json:"game_mode"`
	Version    *int  `json:"version"`
	ODData     struct {
		HasParsed bool `json:"has_parsed"`
	} `json:"od_data"`
	Players []MatchPlayer `json:"players"`
}

func (m *Match) Parsed() bool { return m.ODData.HasParsed || m.Version != nil }

type Benchmark struct {
	Raw float64 `json:"raw"`
	Pct float64 `json:"pct"`
}

type MatchPlayer struct {
	AccountID              int64                `json:"account_id"`
	PlayerSlot             int                  `json:"player_slot"`
	IsRadiant              bool                 `json:"isRadiant"`
	Win                    int                  `json:"win"`
	HeroID                 int                  `json:"hero_id"`
	LaneRole               int                  `json:"lane_role"`
	IsRoaming              bool                 `json:"is_roaming"`
	NetWorth               int                  `json:"net_worth"`
	HeroDamage             int                  `json:"hero_damage"`
	TowerDamage            int                  `json:"tower_damage"`
	ObsPlaced              int                  `json:"obs_placed"`
	SenPlaced              int                  `json:"sen_placed"`
	CampsStacked           int                  `json:"camps_stacked"`
	Stuns                  float64              `json:"stuns"`
	TeamfightParticipation float64              `json:"teamfight_participation"`
	GoldPerMin             int                  `json:"gold_per_min"`
	XPPerMin               int                  `json:"xp_per_min"`
	LastHits               int                  `json:"last_hits"`
	Denies                 int                  `json:"denies"`
	Kills                  int                  `json:"kills"`
	Deaths                 int                  `json:"deaths"`
	Assists                int                  `json:"assists"`
	RankTier               int                  `json:"rank_tier"`
	LHT                    []int                `json:"lh_t"`
	FirstPurchaseTime      map[string]int       `json:"first_purchase_time"`
	DeathsLog              []struct{ Time int } `json:"deaths_log"`
	Benchmarks             map[string]Benchmark `json:"benchmarks"`
}

// Lane roles as OpenDota reports them.
const (
	LaneSafe    = 1
	LaneMid     = 2
	LaneOff     = 3
	LaneJungle  = 4
	parsePollAt = time.Minute
)

// PlayerDetail is one player's match as the trainer stores and reviews it.
type PlayerDetail struct {
	Parsed                 bool
	Radiant, Win           bool
	HeroID                 int
	LaneRole               int
	Roaming                bool
	Kills, Deaths, Assists int
	LastHits, Denies       int
	GPM, XPM               int
	NetWorth               int
	HeroDamage             int
	TowerDamage            int
	ObsPlaced, SenPlaced   int
	CampsStacked           int
	Stuns                  float64
	TeamfightParticipation float64
	RankTier               int
	LastHitsAt             map[int]int
	DeathTimes             []int
	ItemTimes              map[string]int
	Percentiles            map[string]float64
	Allies, Enemies        []int
	LaneOpponents          []int
	NetWorthRank           int
}

// Extract finds the player by account id, or by hero when the profile is private, and
// flattens their row plus who they played with and against.
func Extract(m *Match, accountID int64, heroID int) (PlayerDetail, bool) {
	i := slices.IndexFunc(m.Players, func(p MatchPlayer) bool { return accountID != 0 && p.AccountID == accountID })
	if i < 0 {
		i = slices.IndexFunc(m.Players, func(p MatchPlayer) bool { return heroID != 0 && p.HeroID == heroID })
	}
	if i < 0 {
		return PlayerDetail{}, false
	}
	p := m.Players[i]
	d := PlayerDetail{
		Parsed: m.Parsed(), Radiant: p.IsRadiant, Win: p.IsRadiant == m.RadiantWin, HeroID: p.HeroID,
		LaneRole: p.LaneRole, Roaming: p.IsRoaming, Kills: p.Kills, Deaths: p.Deaths, Assists: p.Assists,
		LastHits: p.LastHits, Denies: p.Denies, GPM: p.GoldPerMin, XPM: p.XPPerMin, NetWorth: p.NetWorth,
		HeroDamage: p.HeroDamage, TowerDamage: p.TowerDamage, ObsPlaced: p.ObsPlaced, SenPlaced: p.SenPlaced,
		CampsStacked: p.CampsStacked, Stuns: p.Stuns, TeamfightParticipation: p.TeamfightParticipation,
		RankTier: p.RankTier, LastHitsAt: map[int]int{}, ItemTimes: p.FirstPurchaseTime, Percentiles: map[string]float64{},
		NetWorthRank: 1,
	}
	for _, minute := range []int{5, 10, 15, 20, 30} {
		if minute < len(p.LHT) {
			d.LastHitsAt[minute] = p.LHT[minute]
		}
	}
	for _, death := range p.DeathsLog {
		d.DeathTimes = append(d.DeathTimes, death.Time)
	}
	for name, b := range p.Benchmarks {
		d.Percentiles[name] = b.Pct
	}
	for j, q := range m.Players {
		switch {
		case j == i:
		case q.IsRadiant == p.IsRadiant:
			d.Allies = append(d.Allies, q.HeroID)
			if q.NetWorth > p.NetWorth {
				d.NetWorthRank++
			}
		default:
			d.Enemies = append(d.Enemies, q.HeroID)
			if opposes(p.LaneRole, q.LaneRole) {
				d.LaneOpponents = append(d.LaneOpponents, q.HeroID)
			}
		}
	}
	return d, true
}

// opposes reports whether two lane roles on opposite teams share a lane: each team's safe
// lane is the other team's offlane.
func opposes(mine, theirs int) bool {
	switch mine {
	case LaneSafe:
		return theirs == LaneOff
	case LaneOff:
		return theirs == LaneSafe
	case LaneMid:
		return theirs == LaneMid
	}
	return false
}

// RequestParse asks OpenDota to download and parse the replay.
func (c *Client) RequestParse(ctx context.Context, matchID string) error {
	_, err := c.do(ctx, http.MethodPost, "/request/"+matchID)
	return err
}

// Match fetches a match. Parsed matches never change, so they are cached on disk.
func (c *Client) Match(ctx context.Context, matchID string) (*Match, error) {
	if _, err := strconv.ParseInt(matchID, 10, 64); err != nil {
		return nil, fmt.Errorf("not an OpenDota match id: %q", matchID)
	}
	return cached(c, filepath.Join("matches", matchID+".json"), forever,
		func() ([]byte, error) { return c.fetch(ctx, "/matches/"+matchID) },
		(*Match).Parsed) // an unparsed match changes once OpenDota parses it
}

// parseRetryAt is how often the parse is requested again while waiting.
const parseRetryAt = 10 * time.Minute

// WaitParsed requests a parse and polls until the match is parsed or ctx ends, asking again
// every 10 minutes in case the request was dropped. progress, if set, is called on each poll
// with how long the wait has taken. It returns the unparsed match if ctx ends first.
func (c *Client) WaitParsed(ctx context.Context, matchID string, progress func(waited time.Duration)) (*Match, error) {
	m, err := c.Match(ctx, matchID)
	if err == nil && m.Parsed() {
		return m, nil
	}
	started := time.Now()
	request := func() {
		if err := c.RequestParse(ctx, matchID); err != nil {
			c.log.Warn("OpenDota parse request failed", "match", matchID, "err", err)
		}
	}
	request()
	tick := time.NewTicker(parsePollAt)
	defer tick.Stop()
	for polls := 0; ; polls++ {
		select {
		case <-ctx.Done():
			if m != nil {
				return m, nil
			}
			return nil, ctx.Err()
		case <-tick.C:
		}
		if polls > 0 && polls%int(parseRetryAt/parsePollAt) == 0 {
			request()
		}
		next, err := c.Match(ctx, matchID)
		if err != nil {
			continue
		}
		m = next
		if m.Parsed() {
			return m, nil
		}
		if progress != nil {
			progress(time.Since(started))
		}
	}
}

type PlayerMatch struct {
	MatchID    int64 `json:"match_id"`
	PlayerSlot int   `json:"player_slot"`
	HeroID     int   `json:"hero_id"`
	StartTime  int64 `json:"start_time"`
	Duration   int   `json:"duration"`
}

// PlayerMatches lists the player's most recent normal matches, newest first.
func (c *Client) PlayerMatches(ctx context.Context, accountID string, limit int) ([]PlayerMatch, error) {
	data, err := c.fetch(ctx, fmt.Sprintf("/players/%s/matches?limit=%d&significant=1", accountID, limit))
	if err != nil {
		return nil, err
	}
	var out []PlayerMatch
	return out, json.Unmarshal(data, &out)
}

// HasPublicMatches reports whether OpenDota lists any matches for the account. It lists
// none when "Expose Public Match Data" is off in Dota.
func (c *Client) HasPublicMatches(ctx context.Context, accountID string) (bool, error) {
	recent, err := c.PlayerMatches(ctx, accountID, 1)
	return len(recent) > 0, err
}
