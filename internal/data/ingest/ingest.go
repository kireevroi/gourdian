// Package matchdata merges OpenDota's parsed match data into the trainer's statistics: after
// each live match, and when importing the player's recent history.
package ingest

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strconv"
	"time"

	"gourdian/internal/data/opendota"
	"gourdian/internal/data/stats"
	"gourdian/internal/game/dota"
	"gourdian/internal/game/model"
)

type Service struct {
	Data  *opendota.Client
	Stats *stats.Store
	Log   *slog.Logger
}

// Detail is a parsed match with names resolved, ready for a review prompt.
type Detail struct {
	opendota.PlayerDetail
	LaneRoleName  string
	Enemies       []string
	Allies        []string
	LaneOpponents []string
	CoreItems     []ItemTime
}

type ItemTime struct {
	Name string
	Time int
}

func (s Service) names(ids []int) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		out = append(out, s.Data.HeroName(id))
	}
	return out
}

func (s Service) detail(d opendota.PlayerDetail) *Detail {
	out := &Detail{PlayerDetail: d, LaneRoleName: dota.LaneNumbered(d.LaneRole), Enemies: s.names(d.Enemies),
		Allies: s.names(d.Allies), LaneOpponents: s.names(d.LaneOpponents)}
	if d.Roaming {
		out.LaneRoleName = "roaming"
	}
	items := s.Data.Items()
	for name, t := range d.ItemTimes {
		if info, ok := items[name]; ok && info.Cost >= dota.CoreItemCost && info.Tier == 0 {
			out.CoreItems = append(out.CoreItems, ItemTime{Name: info.DName, Time: t})
		}
	}
	slices.SortFunc(out.CoreItems, func(a, b ItemTime) int { return a.Time - b.Time })
	return out
}

// Enrich returns the detail with Parsed false when the parse doesn't arrive before ctx ends.
// progress, if set, is called while waiting with how long it has taken.
func (s Service) Enrich(ctx context.Context, m model.MatchSummary, accountID string, progress func(time.Duration)) (*Detail, error) {
	match, err := s.Data.WaitParsed(ctx, m.MatchID, progress)
	if err != nil {
		return nil, err
	}
	acct, _ := strconv.ParseInt(accountID, 10, 64)
	d, ok := opendota.Extract(match, acct, m.HeroID)
	if !ok {
		return nil, fmt.Errorf("player not found in OpenDota match %s", m.MatchID)
	}
	if !d.Parsed {
		return s.detail(d), nil
	}
	enemies := s.names(d.Enemies)
	err = s.Stats.UpdateMatch(m.MatchID, func(row *model.MatchSummary) {
		applyDetail(row, d, enemies)
	})
	if err != nil {
		return nil, err
	}
	if err := s.Stats.AppendItems(itemTimings(m.MatchID, m.Hero, d, s.Data.Items())); err != nil {
		s.Log.Warn("save item timings", "err", err)
	}
	return s.detail(d), nil
}

func applyDetail(row *model.MatchSummary, d opendota.PlayerDetail, enemies []string) {
	row.Parsed, row.LaneRole, row.NetWorth, row.HeroDamage, row.TowerDamage = true, d.LaneRole, d.NetWorth, d.HeroDamage, d.TowerDamage
	row.ObsPlaced, row.SenPlaced, row.CampsStacked, row.TeamfightParticipation = d.ObsPlaced, d.SenPlaced, d.CampsStacked, d.TeamfightParticipation
	row.GPMPct, row.LHPct, row.HeroDamagePct = d.Percentiles["gold_per_min"], d.Percentiles["last_hits_per_min"], d.Percentiles["hero_damage_per_min"]
	row.EnemyHeroes = enemies
	if row.RankTier == 0 {
		row.RankTier = d.RankTier
	}
	if row.LastHitsAt == nil {
		row.LastHitsAt = map[string]int{}
	}
	for minute, lh := range d.LastHitsAt {
		key := fmt.Sprintf("%d:00", minute)
		if _, ok := row.LastHitsAt[key]; !ok {
			row.LastHitsAt[key] = lh
		}
	}
}

func itemTimings(matchID, hero string, d opendota.PlayerDetail, items map[string]opendota.ItemInfo) []model.ItemTiming {
	var out []model.ItemTiming
	for name, t := range opendota.CoreItemTimes(d.ItemTimes, items, dota.CoreItemCost) {
		out = append(out, model.ItemTiming{MatchID: matchID, Hero: hero, Item: name, Time: t, Source: model.SourceOpenDota})
	}
	slices.SortFunc(out, func(a, b model.ItemTiming) int { return a.Time - b.Time })
	return out
}

// roleFor guesses a position from lane and net worth rank within the team, since OpenDota
// doesn't record which position someone queued as.
func roleFor(d opendota.PlayerDetail) string {
	switch {
	case d.LaneRole == opendota.LaneMid:
		return dota.Mid
	case d.LaneRole == opendota.LaneSafe && d.NetWorthRank <= 2:
		return dota.Carry
	case d.LaneRole == opendota.LaneOff && d.NetWorthRank <= 3:
		return dota.Offlane
	case d.LaneRole == opendota.LaneOff || d.Roaming:
		return dota.SoftSupport
	case d.LaneRole == 0 && d.NetWorthRank <= 2:
		return dota.Carry
	default:
		return dota.HardSupport
	}
}

// ImportProgress is reported after each match considered.
type ImportProgress struct {
	Done, Total, Added int
}

// Import adds the player's recent matches that aren't recorded yet.
func (s Service) Import(ctx context.Context, accountID string, n int, progress func(ImportProgress)) (int, error) {
	acct, err := strconv.ParseInt(accountID, 10, 64)
	if err != nil || acct == 0 {
		return 0, errors.New("no Steam account id yet: play a match with the trainer running, or set it in Settings")
	}
	s.Data.WaitReady(ctx)
	recent, err := s.Data.PlayerMatches(ctx, accountID, n)
	if err != nil {
		return 0, fmt.Errorf("list matches from OpenDota: %w", err)
	}
	known := map[string]bool{}
	if existing, err := s.Stats.Matches(); err == nil {
		for _, m := range existing {
			known[m.MatchID] = true
		}
	}
	p := ImportProgress{Total: len(recent)}
	for i := len(recent) - 1; i >= 0; i-- {
		pm := recent[i]
		id := strconv.FormatInt(pm.MatchID, 10)
		if !known[id] {
			if err := s.importOne(ctx, id, acct, pm); err != nil {
				if ctx.Err() != nil {
					return p.Added, ctx.Err()
				}
				s.Log.Warn("skipping match", "match", id, "err", err)
			} else {
				p.Added++
			}
		}
		p.Done++
		if progress != nil {
			progress(p)
		}
	}
	return p.Added, nil
}

func (s Service) importOne(ctx context.Context, id string, acct int64, pm opendota.PlayerMatch) error {
	match, err := s.Data.Match(ctx, id)
	if err != nil {
		return err
	}
	d, ok := opendota.Extract(match, acct, pm.HeroID)
	if !ok {
		return errors.New("player not in match")
	}
	hero := s.Data.HeroName(d.HeroID)
	result := "loss"
	if d.Win {
		result = "win"
	}
	team := "dire"
	if d.Radiant {
		team = "radiant"
	}
	m := model.MatchSummary{
		MatchID: id, Source: model.SourceOpenDota, HeroID: d.HeroID, Hero: hero, Role: roleFor(d), Team: team, Result: result,
		EndedAt:     time.Unix(match.StartTime+int64(match.Duration), 0),
		DurationSec: match.Duration, Kills: d.Kills, Deaths: d.Deaths, Assists: d.Assists, LastHits: d.LastHits,
		Denies: d.Denies, GPM: d.GPM, XPM: d.XPM, RankTier: d.RankTier, DeathClocks: d.DeathTimes,
		LastHitsAt: map[string]int{}, TipCounts: map[string]int{},
	}
	if d.Parsed {
		applyDetail(&m, d, s.names(d.Enemies))
	}
	if err := s.Stats.AppendMatch(m); err != nil {
		return err
	}
	return s.Stats.AppendItems(itemTimings(id, hero, d, s.Data.Items()))
}
