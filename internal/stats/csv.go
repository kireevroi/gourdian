package stats

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Older versions kept everything in these CSV files. They are read once, into the data file,
// and written again by Export so spreadsheets still work.
const (
	MatchesFile  = "matches.csv"
	TimelineFile = "timeline.csv"
	TipsFile     = "tips.csv"
	MMRFile      = "mmr.csv"
	ReviewsFile  = "reviews.csv"
	ItemsFile    = "items.csv"
	GoalsFile    = "goals.csv"
)

var Files = []string{MatchesFile, TimelineFile, TipsFile, MMRFile, ReviewsFile, ItemsFile, GoalsFile}

// importCSV moves the CSV files of an older version into the data file, once. It all happens
// in one transaction, together with the flag that it's done, so a failure leaves nothing behind
// and the next start tries again.
func (s *Store) importCSV() error {
	done, err := s.meta("csv_imported")
	if err != nil || done != "" {
		return err
	}
	var rows [7][]map[string]string
	for i, name := range []string{MatchesFile, TimelineFile, TipsFile, ItemsFile, MMRFile, ReviewsFile, GoalsFile} {
		if rows[i], err = readRows(s.path(name)); err != nil {
			return err
		}
	}
	matches, samples, tips, items, mmr, reviews, goals := rows[0], rows[1], rows[2], rows[3], rows[4], rows[5], rows[6]

	sampleBatch := make([]Sample, 0, len(samples))
	for _, r := range samples {
		sampleBatch = append(sampleBatch, Sample{
			MatchID: r["match_id"], Clock: atoi(r["clock"]), Gold: atoi(r["gold"]), GPM: atoi(r["gpm"]), XPM: atoi(r["xpm"]),
			LastHits: atoi(r["last_hits"]), Denies: atoi(r["denies"]), Kills: atoi(r["kills"]), Deaths: atoi(r["deaths"]),
			Assists: atoi(r["assists"]), Level: atoi(r["level"]), Alive: r["alive"] == "true",
		})
	}
	tipBatch := make([]TipRecord, 0, len(tips))
	for _, r := range tips {
		tipBatch = append(tipBatch, TipRecord{
			At: parseTime(r["at"]), MatchID: r["match_id"], Clock: atoi(r["clock"]), Rule: r["rule"],
			Category: r["category"], Severity: r["severity"], Habit: r["habit"] == "true", Text: r["text"],
		})
	}
	itemBatch := make([]ItemTiming, 0, len(items))
	for _, r := range items {
		itemBatch = append(itemBatch, ItemTiming{MatchID: r["match_id"], Hero: r["hero"], Item: r["item"], Time: atoi(r["time"]), Source: r["source"]})
	}
	goalBatch := make([]Goal, 0, len(goals))
	for _, r := range goals {
		target, _ := strconv.ParseFloat(r["target"], 64)
		goalBatch = append(goalBatch, Goal{Created: parseTime(r["created"]), Week: r["week"], Metric: r["metric"],
			Comparator: r["comparator"], Target: target, Label: r["label"], MatchID: r["match_id"]})
	}

	return s.tx(func(tx *sql.Tx) error {
		for _, r := range matches {
			if err := appendMatch(tx, matchFromRow(r)); err != nil && !errors.Is(err, ErrDuplicate) {
				return err
			}
		}
		if err := appendSamples(tx, sampleBatch); err != nil {
			return err
		}
		if err := appendTips(tx, tipBatch); err != nil {
			return err
		}
		if err := appendItems(tx, itemBatch); err != nil {
			return err
		}
		for _, r := range mmr {
			if err := appendMMR(tx, MMREntry{Date: parseTime(r["date"]), MMR: atoi(r["mmr"]), Note: r["note"]}); err != nil {
				return err
			}
		}
		for _, r := range reviews {
			if err := appendReview(tx, Review{
				Date: parseTime(r["date"]), MatchID: r["match_id"], Hero: r["hero"], HeroID: atoi(r["hero_id"]),
				Role: r["role"], Result: r["result"], Summary: r["summary"], Strengths: splitList(r["strengths"]),
				Improve: splitList(r["improve"]), NextGameFocus: r["next_game_focus"],
			}); err != nil {
				return err
			}
		}
		if err := appendGoals(tx, goalBatch); err != nil {
			return err
		}
		return setMeta(tx, "csv_imported", time.Now().Format(time.RFC3339))
	})
}

// Export writes every table to the CSV files, for opening in a spreadsheet.
func (s *Store) Export() (string, error) {
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return "", err
	}
	matches, err := s.Matches()
	if err != nil {
		return "", err
	}
	header := append([]string{}, matchColumns...)
	rows := make([]map[string]string, 0, len(matches))
	for _, m := range matches {
		h, row := matchRow(m)
		for _, col := range h {
			if !contains(header, col) {
				header = append(header, col)
			}
		}
		rows = append(rows, row)
	}
	if err := writeAll(s.path(MatchesFile), header, rows); err != nil {
		return "", err
	}

	samples, err := s.Timeline()
	if err != nil {
		return "", err
	}
	rows = rows[:0]
	for _, x := range samples {
		rows = append(rows, map[string]string{
			"match_id": x.MatchID, "clock": itoa(x.Clock), "gold": itoa(x.Gold), "gpm": itoa(x.GPM), "xpm": itoa(x.XPM),
			"last_hits": itoa(x.LastHits), "denies": itoa(x.Denies), "kills": itoa(x.Kills), "deaths": itoa(x.Deaths),
			"assists": itoa(x.Assists), "level": itoa(x.Level), "alive": strconv.FormatBool(x.Alive),
		})
	}
	if err := writeAll(s.path(TimelineFile), sampleColumns, rows); err != nil {
		return "", err
	}

	items, err := s.Items()
	if err != nil {
		return "", err
	}
	rows = rows[:0]
	for _, it := range items {
		rows = append(rows, map[string]string{"match_id": it.MatchID, "hero": it.Hero, "item": it.Item,
			"time": itoa(it.Time), "source": it.Source})
	}
	if err := writeAll(s.path(ItemsFile), itemColumns, rows); err != nil {
		return "", err
	}

	mmr, err := s.MMR()
	if err != nil {
		return "", err
	}
	rows = rows[:0]
	for _, e := range mmr {
		rows = append(rows, map[string]string{"date": timeValue(e.Date), "mmr": itoa(e.MMR), "note": e.Note, "match_id": e.MatchID})
	}
	if err := writeAll(s.path(MMRFile), mmrColumns, rows); err != nil {
		return "", err
	}

	reviews, err := s.Reviews()
	if err != nil {
		return "", err
	}
	rows = rows[:0]
	for _, r := range reviews {
		rows = append(rows, map[string]string{"date": timeValue(r.Date), "match_id": r.MatchID, "hero": r.Hero,
			"hero_id": itoa(r.HeroID), "role": r.Role, "result": r.Result, "summary": r.Summary,
			"strengths": joinList(r.Strengths), "improve": joinList(r.Improve), "next_game_focus": r.NextGameFocus})
	}
	if err := writeAll(s.path(ReviewsFile), reviewColumns, rows); err != nil {
		return "", err
	}

	goals, err := s.Goals()
	if err != nil {
		return "", err
	}
	rows = rows[:0]
	for _, g := range goals {
		rows = append(rows, map[string]string{"created": timeValue(g.Created), "week": g.Week, "metric": g.Metric,
			"comparator": g.Comparator, "target": strconv.FormatFloat(g.Target, 'f', -1, 64), "label": g.Label,
			"match_id": g.MatchID})
	}
	if err := writeAll(s.path(GoalsFile), goalColumns, rows); err != nil {
		return "", err
	}

	tips, err := s.tipRecords()
	if err != nil {
		return "", err
	}
	rows = rows[:0]
	for _, t := range tips {
		rows = append(rows, map[string]string{"at": timeValue(t.At), "match_id": t.MatchID, "clock": itoa(t.Clock),
			"rule": t.Rule, "category": t.Category, "severity": t.Severity, "habit": strconv.FormatBool(t.Habit), "text": t.Text})
	}
	if err := writeAll(s.path(TipsFile), tipColumns, rows); err != nil {
		return "", err
	}
	return s.dir, nil
}

func (s *Store) tipRecords() ([]TipRecord, error) {
	rows, err := s.db.Query(`SELECT at, match_id, clock, rule, category, severity, habit, text FROM tips ORDER BY rowid`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TipRecord
	for rows.Next() {
		var t TipRecord
		var at string
		var habit int
		if err := rows.Scan(&at, &t.MatchID, &t.Clock, &t.Rule, &t.Category, &t.Severity, &habit, &t.Text); err != nil {
			return nil, err
		}
		t.At, t.Habit = parseTime(at), habit == 1
		out = append(out, t)
	}
	return out, rows.Err()
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// matchRow is the CSV shape of a match, kept for the export.
func matchRow(m MatchSummary) ([]string, map[string]string) {
	row := map[string]string{
		"ended_at": timeValue(m.EndedAt), "match_id": m.MatchID, "source": m.Source, "hero_id": itoa(m.HeroID),
		"hero": m.Hero, "role": m.Role, "team": m.Team, "result": m.Result, "duration_sec": itoa(m.DurationSec),
		"kills": itoa(m.Kills), "deaths": itoa(m.Deaths), "assists": itoa(m.Assists), "last_hits": itoa(m.LastHits),
		"denies": itoa(m.Denies), "gpm": itoa(m.GPM), "xpm": itoa(m.XPM), "rank_tier": itoa(m.RankTier),
		"simulated": strconv.FormatBool(m.Simulated), "ranked": strconv.FormatBool(m.Ranked),
		"parsed": strconv.FormatBool(m.Parsed),
	}
	if m.Parsed {
		for col, v := range map[string]string{
			"lane_role": itoa(m.LaneRole), "net_worth": itoa(m.NetWorth), "hero_damage": itoa(m.HeroDamage),
			"tower_damage": itoa(m.TowerDamage), "obs_placed": itoa(m.ObsPlaced), "sen_placed": itoa(m.SenPlaced),
			"camps_stacked": itoa(m.CampsStacked), "teamfight_participation": ftoa(m.TeamfightParticipation),
			"gpm_pct": ftoa(m.GPMPct), "lh_pct": ftoa(m.LHPct), "hero_damage_pct": ftoa(m.HeroDamagePct),
			"enemy_heroes": strings.Join(m.EnemyHeroes, ";"),
		} {
			row[col] = v
		}
	}
	for _, minute := range []int{5, 10, 15, 20, 30} {
		if lh, ok := m.LastHitsAt[fmt.Sprintf("%d:00", minute)]; ok {
			row[fmt.Sprintf("lh_%d", minute)] = itoa(lh)
		}
	}
	var deaths []string
	for _, d := range m.DeathClocks {
		deaths = append(deaths, itoa(d))
	}
	row["death_clocks"] = strings.Join(deaths, ";")
	header := append([]string{}, matchColumns...)
	total := 0
	for _, rule := range sortedKeys(m.TipCounts) {
		col := "mistakes_" + rule
		header = append(header, col)
		row[col] = itoa(m.TipCounts[rule])
		total += m.TipCounts[rule]
	}
	row["mistakes_total"] = itoa(total)
	return header, row
}
