// Package stats records matches, per-minute samples, tips and MMR as CSV files that
// open in any spreadsheet tool. Files are read by column name, so columns can be added
// later without breaking older files.
package stats

import (
	"database/sql"
	"encoding/csv"
	"errors"
	"fmt"
	"gourdian/internal/model"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

// ErrDuplicate means the match was already recorded, for example by replaying a recording.
var ErrDuplicate = errors.New("match already recorded")

// ErrNoMatch means no match with that id is recorded.
var ErrNoMatch = errors.New("match not recorded")

type Store struct {
	dir     string
	db      *sql.DB
	onError func(msg string, err error)
	history atomic.Int64 // counts changes to the matches, for HistoryVersion
}

// Dir is the folder the CSV exports are written to.
func (s *Store) Dir() string { return s.dir }

func (s *Store) path(name string) string { return filepath.Join(s.dir, name) }

var matchColumns = []string{"ended_at", "match_id", "source", "hero_id", "hero", "role", "team", "result", "duration_sec",
	"kills", "deaths", "assists", "last_hits", "denies", "gpm", "xpm", "lh_5", "lh_10", "lh_15", "lh_20", "lh_30",
	"death_clocks", "mistakes_total", "rank_tier", "simulated",
	"parsed", "lane_role", "net_worth", "hero_damage", "tower_damage", "obs_placed", "sen_placed", "camps_stacked",
	"teamfight_participation", "gpm_pct", "lh_pct", "hero_damage_pct", "enemy_heroes"}

func ftoa(f float64) string { return strconv.FormatFloat(f, 'f', 3, 64) }

func atof(s string) float64 {
	f, _ := strconv.ParseFloat(s, 64)
	return f
}

func matchFromRow(r map[string]string) model.MatchSummary {
	m := model.MatchSummary{
		MatchID: r["match_id"], Source: r["source"], HeroID: atoi(r["hero_id"]), Hero: r["hero"], Role: r["role"],
		Team: r["team"], Result: r["result"], DurationSec: atoi(r["duration_sec"]), Kills: atoi(r["kills"]),
		Deaths: atoi(r["deaths"]), Assists: atoi(r["assists"]), LastHits: atoi(r["last_hits"]), Denies: atoi(r["denies"]),
		GPM: atoi(r["gpm"]), XPM: atoi(r["xpm"]), RankTier: atoi(r["rank_tier"]), Simulated: r["simulated"] == "true",
		Parsed: r["parsed"] == "true", LaneRole: atoi(r["lane_role"]), NetWorth: atoi(r["net_worth"]),
		HeroDamage: atoi(r["hero_damage"]), TowerDamage: atoi(r["tower_damage"]), ObsPlaced: atoi(r["obs_placed"]),
		SenPlaced: atoi(r["sen_placed"]), CampsStacked: atoi(r["camps_stacked"]),
		TeamfightParticipation: atof(r["teamfight_participation"]), GPMPct: atof(r["gpm_pct"]), LHPct: atof(r["lh_pct"]),
		HeroDamagePct: atof(r["hero_damage_pct"]), EnemyHeroes: splitNonEmpty(r["enemy_heroes"], ";"),
		LastHitsAt: map[string]int{}, TipCounts: map[string]int{},
	}
	if m.Source == "" {
		m.Source = model.SourceLive
		if m.Simulated {
			m.Source = model.SourceSim
		}
	}
	m.EndedAt, _ = time.Parse(time.RFC3339, r["ended_at"])
	for col, v := range r {
		switch {
		case v == "":
		case strings.HasPrefix(col, "lh_"):
			m.LastHitsAt[strings.TrimPrefix(col, "lh_")+":00"] = atoi(v)
		case strings.HasPrefix(col, "mistakes_") && col != "mistakes_total":
			if n := atoi(v); n > 0 {
				m.TipCounts[strings.TrimPrefix(col, "mistakes_")] = n
			}
		}
	}
	for _, d := range splitNonEmpty(r["death_clocks"], ";") {
		m.DeathClocks = append(m.DeathClocks, atoi(d))
	}
	return m
}

func splitNonEmpty(s, sep string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, sep)
}

var itemColumns = []string{"match_id", "hero", "item", "time", "source"}

var sampleColumns = []string{"match_id", "clock", "gold", "gpm", "xpm", "last_hits", "denies", "kills", "deaths", "assists", "level", "alive"}

var tipColumns = []string{"at", "match_id", "clock", "rule", "category", "severity", "habit", "text"}

var mmrColumns = []string{"date", "mmr", "note"}

var reviewColumns = []string{"date", "match_id", "hero", "hero_id", "role", "result", "summary", "strengths", "improve", "next_game_focus"}

const listSep = " | "

func joinList(items []string) string {
	clean := make([]string, len(items))
	for i, it := range items {
		clean[i] = strings.ReplaceAll(it, "|", "/")
	}
	return strings.Join(clean, listSep)
}

func splitList(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, listSep)
}

func writeAll(path string, header []string, rows []map[string]string) error {
	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	w := csv.NewWriter(f)
	w.Write(header)
	for _, r := range rows {
		w.Write(project(header, r))
	}
	w.Flush()
	if err := errors.Join(w.Error(), f.Close()); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func readRows(path string) ([]map[string]string, error) {
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	r := csv.NewReader(f)
	r.FieldsPerRecord = -1
	records, err := r.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", filepath.Base(path), err)
	}
	if len(records) == 0 {
		return nil, nil
	}
	header := records[0]
	out := make([]map[string]string, 0, len(records)-1)
	for _, rec := range records[1:] {
		row := make(map[string]string, len(header))
		for i, col := range header {
			if i < len(rec) {
				row[col] = rec[i]
			}
		}
		out = append(out, row)
	}
	return out, nil
}

func project(header []string, row map[string]string) []string {
	out := make([]string, len(header))
	for i, col := range header {
		out[i] = row[col]
	}
	return out
}

func sortedKeys(m map[string]int) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

func itoa(n int) string { return strconv.Itoa(n) }

func atoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}
