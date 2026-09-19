package stats

import (
	"database/sql"
	"fmt"
)

const matchColumnList = `match_id, ended_at, source, hero_id, hero, role, team, result, duration_sec, kills, deaths,
	assists, last_hits, denies, gpm, xpm, rank_tier, simulated, ranked, parsed, lane_role, net_worth, hero_damage,
	tower_damage, obs_placed, sen_placed, camps_stacked, teamfight, gpm_pct, lh_pct, hero_damage_pct, enemy_heroes,
	last_hits_at, death_clocks, tip_counts`

func matchValues(m MatchSummary) []any {
	return []any{m.MatchID, timeValue(m.EndedAt), m.Source, m.HeroID, m.Hero, m.Role, m.Team, m.Result, m.DurationSec,
		m.Kills, m.Deaths, m.Assists, m.LastHits, m.Denies, m.GPM, m.XPM, m.RankTier, boolInt(m.Simulated),
		boolInt(m.Ranked), boolInt(m.Parsed), m.LaneRole, m.NetWorth, m.HeroDamage, m.TowerDamage, m.ObsPlaced,
		m.SenPlaced, m.CampsStacked, m.TeamfightParticipation, m.GPMPct, m.LHPct, m.HeroDamagePct,
		jsonValue(m.EnemyHeroes), jsonValue(m.LastHitsAt), jsonValue(m.DeathClocks), jsonValue(m.TipCounts)}
}

func scanMatch(rows *sql.Rows) (MatchSummary, error) {
	var m MatchSummary
	var endedAt, enemies, lastHits, deaths, counts string
	var simulated, ranked, parsed int
	err := rows.Scan(&m.MatchID, &endedAt, &m.Source, &m.HeroID, &m.Hero, &m.Role, &m.Team, &m.Result, &m.DurationSec,
		&m.Kills, &m.Deaths, &m.Assists, &m.LastHits, &m.Denies, &m.GPM, &m.XPM, &m.RankTier, &simulated, &ranked,
		&parsed, &m.LaneRole, &m.NetWorth, &m.HeroDamage, &m.TowerDamage, &m.ObsPlaced, &m.SenPlaced, &m.CampsStacked,
		&m.TeamfightParticipation, &m.GPMPct, &m.LHPct, &m.HeroDamagePct, &enemies, &lastHits, &deaths, &counts)
	if err != nil {
		return m, err
	}
	m.EndedAt = parseTime(endedAt)
	m.Simulated, m.Ranked, m.Parsed = simulated == 1, ranked == 1, parsed == 1
	m.EnemyHeroes = stringsFromJSON(enemies)
	m.LastHitsAt = countsFromJSON(lastHits)
	m.DeathClocks = intsFromJSON(deaths)
	m.TipCounts = countsFromJSON(counts)
	if m.Source == "" {
		m.Source = SourceLive
		if m.Simulated {
			m.Source = SourceSim
		}
	}
	return m, nil
}

func (s *Store) AppendMatch(m MatchSummary) error {
	var seen int
	s.db.QueryRow(`SELECT 1 FROM matches WHERE match_id = ?`, m.MatchID).Scan(&seen)
	if seen == 1 {
		return ErrDuplicate
	}
	_, err := s.db.Exec(`INSERT INTO matches (`+matchColumnList+`) VALUES (`+placeholders(35)+`)`, matchValues(m)...)
	return err
}

// UpdateMatch changes one match, for example to add OpenDota's data after the game.
func (s *Store) UpdateMatch(matchID string, update func(*MatchSummary)) error {
	return s.tx(func(tx *sql.Tx) error {
		var rowID int64
		if err := tx.QueryRow(`SELECT rowid FROM matches WHERE match_id = ?`, matchID).Scan(&rowID); err != nil {
			return fmt.Errorf("match %s not recorded", matchID)
		}
		rows, err := tx.Query(`SELECT `+matchColumnList+` FROM matches WHERE match_id = ?`, matchID)
		if err != nil {
			return err
		}
		if !rows.Next() {
			rows.Close()
			return fmt.Errorf("match %s not recorded", matchID)
		}
		m, err := scanMatch(rows)
		rows.Close()
		if err != nil {
			return err
		}
		update(&m)
		m.MatchID = matchID
		// The row keeps its rowid, so matches stay in the order they were played.
		_, err = tx.Exec(`REPLACE INTO matches (rowid, `+matchColumnList+`) VALUES (?, `+placeholders(35)+`)`,
			append([]any{rowID}, matchValues(m)...)...)
		return err
	})
}

func (s *Store) queryMatches(where string, args ...any) ([]MatchSummary, error) {
	rows, err := s.db.Query(`SELECT `+matchColumnList+` FROM matches `+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MatchSummary
	for rows.Next() {
		m, err := scanMatch(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// Matches returns every recorded match, oldest first.
func (s *Store) Matches() ([]MatchSummary, error) {
	return s.queryMatches(`ORDER BY julianday(ended_at), rowid`)
}

// Recent returns up to n matches, newest first.
func (s *Store) Recent(n int) ([]MatchSummary, error) {
	return s.queryMatches(`ORDER BY julianday(ended_at) DESC, rowid DESC LIMIT ?`, n)
}

// Match returns one match by id.
func (s *Store) Match(matchID string) (MatchSummary, error) {
	found, err := s.queryMatches(`WHERE match_id = ?`, matchID)
	if err != nil {
		return MatchSummary{}, err
	}
	if len(found) == 0 {
		return MatchSummary{}, fmt.Errorf("match %s not recorded", matchID)
	}
	return found[0], nil
}

// AppendItems stores item timings, ignoring ones already known for the same match and source.
func (s *Store) AppendItems(items []ItemTiming) error {
	if len(items) == 0 {
		return nil
	}
	return s.tx(func(tx *sql.Tx) error {
		for _, it := range items {
			_, err := tx.Exec(`INSERT OR IGNORE INTO items (match_id, hero, item, time, source) VALUES (?, ?, ?, ?, ?)`,
				it.MatchID, it.Hero, it.Item, it.Time, it.Source)
			if err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *Store) Items() ([]ItemTiming, error) {
	rows, err := s.db.Query(`SELECT match_id, hero, item, time, source FROM items ORDER BY rowid`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ItemTiming
	for rows.Next() {
		var it ItemTiming
		if err := rows.Scan(&it.MatchID, &it.Hero, &it.Item, &it.Time, &it.Source); err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

func (s *Store) AppendSamples(samples []Sample) error {
	if len(samples) == 0 {
		return nil
	}
	return s.tx(func(tx *sql.Tx) error {
		for _, x := range samples {
			_, err := tx.Exec(`REPLACE INTO samples (match_id, clock, gold, gpm, xpm, last_hits, denies, kills, deaths, assists, level, alive)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				x.MatchID, x.Clock, x.Gold, x.GPM, x.XPM, x.LastHits, x.Denies, x.Kills, x.Deaths, x.Assists, x.Level, boolInt(x.Alive))
			if err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *Store) Timeline() ([]Sample, error) {
	rows, err := s.db.Query(`SELECT match_id, clock, gold, gpm, xpm, last_hits, denies, kills, deaths, assists, level, alive
		FROM samples ORDER BY match_id, clock`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Sample
	for rows.Next() {
		var x Sample
		var alive int
		if err := rows.Scan(&x.MatchID, &x.Clock, &x.Gold, &x.GPM, &x.XPM, &x.LastHits, &x.Denies, &x.Kills,
			&x.Deaths, &x.Assists, &x.Level, &alive); err != nil {
			return nil, err
		}
		x.Alive = alive == 1
		out = append(out, x)
	}
	return out, rows.Err()
}

func (s *Store) AppendTips(tips []TipRecord) error {
	if len(tips) == 0 {
		return nil
	}
	return s.tx(func(tx *sql.Tx) error {
		for _, t := range tips {
			_, err := tx.Exec(`INSERT INTO tips (at, match_id, clock, rule, category, severity, habit, text)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
				timeValue(t.At), t.MatchID, t.Clock, t.Rule, t.Category, t.Severity, boolInt(t.Habit), t.Text)
			if err != nil {
				return err
			}
		}
		return nil
	})
}

// RuleCounts counts the tips each rule gave in the given matches.
func (s *Store) RuleCounts(matchIDs map[string]bool) (map[string]int, error) {
	counts := map[string]int{}
	if len(matchIDs) == 0 {
		return counts, nil
	}
	ids := make([]any, 0, len(matchIDs))
	for id := range matchIDs {
		ids = append(ids, id)
	}
	rows, err := s.db.Query(`SELECT rule, COUNT(*) FROM tips WHERE match_id IN (`+placeholders(len(ids))+`) GROUP BY rule`, ids...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var rule string
		var n int
		if err := rows.Scan(&rule, &n); err != nil {
			return nil, err
		}
		counts[rule] = n
	}
	return counts, rows.Err()
}

// Tips returns every tip ever shown, oldest first.
func (s *Store) Tips() ([]TipRecord, error) { return s.tipRecords() }

// AppendMMR logs an MMR reading. One logged against a match replaces an earlier reading for
// that same match, so correcting a number doesn't leave two.
// FiresIn counts, per rule, how often it fired in each of the given matches. One query
// instead of one per rule: the drill page asks about every habit at once.
func (s *Store) FiresIn(matchIDs []string) (map[string]map[string]int, error) {
	out := map[string]map[string]int{}
	if len(matchIDs) == 0 {
		return out, nil
	}
	args := make([]any, len(matchIDs))
	for i, id := range matchIDs {
		args[i] = id
	}
	rows, err := s.db.Query(`SELECT rule, match_id, COUNT(*) FROM tips WHERE match_id IN (`+placeholders(len(args))+`)
		GROUP BY rule, match_id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var rule, match string
		var n int
		if err := rows.Scan(&rule, &match, &n); err != nil {
			return nil, err
		}
		if out[rule] == nil {
			out[rule] = map[string]int{}
		}
		out[rule][match] = n
	}
	return out, rows.Err()
}

// FiresByMatch counts how often one rule fired in each match it appeared in.
func (s *Store) FiresByMatch(rule string) (map[string]int, error) {
	rows, err := s.db.Query(`SELECT match_id, COUNT(*) FROM tips WHERE rule = ? GROUP BY match_id`, rule)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var id string
		var n int
		if err := rows.Scan(&id, &n); err != nil {
			return nil, err
		}
		out[id] = n
	}
	return out, rows.Err()
}

func (s *Store) AppendMMR(e MMREntry) error {
	if e.MatchID != "" {
		if _, err := s.db.Exec(`DELETE FROM mmr WHERE match_id = ?`, e.MatchID); err != nil {
			return err
		}
	}
	_, err := s.db.Exec(`INSERT INTO mmr (date, mmr, note, match_id) VALUES (?, ?, ?, ?)`,
		timeValue(e.Date), e.MMR, e.Note, e.MatchID)
	return err
}

func (s *Store) MMR() ([]MMREntry, error) {
	rows, err := s.db.Query(`SELECT date, mmr, note, match_id FROM mmr ORDER BY julianday(date), rowid`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MMREntry
	for rows.Next() {
		var e MMREntry
		var date string
		if err := rows.Scan(&date, &e.MMR, &e.Note, &e.MatchID); err != nil {
			return nil, err
		}
		e.Date = parseTime(date)
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *Store) AppendReview(r Review) error {
	_, err := s.db.Exec(`INSERT INTO reviews (date, match_id, hero, hero_id, role, result, summary, strengths, improve, next_game_focus, followed_focus)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		timeValue(r.Date), r.MatchID, r.Hero, r.HeroID, r.Role, r.Result, r.Summary,
		jsonValue(r.Strengths), jsonValue(r.Improve), r.NextGameFocus, r.FollowedFocus)
	return err
}

// Reviews returns every post-match review, oldest first.
func (s *Store) Reviews() ([]Review, error) {
	rows, err := s.db.Query(`SELECT date, match_id, hero, hero_id, role, result, summary, strengths, improve, next_game_focus,
		COALESCE(followed_focus, '') FROM reviews ORDER BY julianday(date), rowid`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Review
	for rows.Next() {
		var r Review
		var date, strengths, improve string
		if err := rows.Scan(&date, &r.MatchID, &r.Hero, &r.HeroID, &r.Role, &r.Result, &r.Summary,
			&strengths, &improve, &r.NextGameFocus, &r.FollowedFocus); err != nil {
			return nil, err
		}
		r.Date = parseTime(date)
		r.Strengths, r.Improve = stringsFromJSON(strengths), stringsFromJSON(improve)
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) AppendGoals(goals []Goal) error {
	if len(goals) == 0 {
		return nil
	}
	return s.tx(func(tx *sql.Tx) error {
		for _, g := range goals {
			_, err := tx.Exec(`INSERT INTO goals (created, week, metric, comparator, target, label, match_id)
				VALUES (?, ?, ?, ?, ?, ?, ?)`,
				timeValue(g.Created), g.Week, g.Metric, g.Comparator, g.Target, g.Label, g.MatchID)
			if err != nil {
				return err
			}
		}
		return nil
	})
}

// Goals returns every goal ever set, oldest first.
func (s *Store) Goals() ([]Goal, error) {
	rows, err := s.db.Query(`SELECT created, week, metric, comparator, target, label, match_id FROM goals ORDER BY julianday(created), rowid`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Goal
	for rows.Next() {
		var g Goal
		var created string
		if err := rows.Scan(&created, &g.Week, &g.Metric, &g.Comparator, &g.Target, &g.Label, &g.MatchID); err != nil {
			return nil, err
		}
		g.Created = parseTime(created)
		out = append(out, g)
	}
	return out, rows.Err()
}

// RuleRow is one stored rule: a rule the player wrote, or their changes to a built-in one.
type RuleRow struct {
	ID   string
	Kind string // "custom" or "override"
	Spec string // JSON
}

// RuleRows returns every stored rule, in the order they were saved.
func (s *Store) RuleRows() ([]RuleRow, error) {
	rows, err := s.db.Query(`SELECT id, kind, spec FROM rules ORDER BY rowid`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RuleRow
	for rows.Next() {
		var r RuleRow
		if err := rows.Scan(&r.ID, &r.Kind, &r.Spec); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ReplaceRules stores the complete set of rules, replacing what was there.
func (s *Store) ReplaceRules(rows []RuleRow) error {
	return s.tx(func(tx *sql.Tx) error {
		if _, err := tx.Exec(`DELETE FROM rules`); err != nil {
			return err
		}
		for _, r := range rows {
			if _, err := tx.Exec(`INSERT INTO rules (id, kind, spec) VALUES (?, ?, ?)`, r.ID, r.Kind, r.Spec); err != nil {
				return err
			}
		}
		return nil
	})
}

// RulesImported reports whether the rules of an older version have been taken over already.
func (s *Store) RulesImported() bool { return s.meta("rules_imported") != "" }

// MarkRulesImported records that the old rules file has been read.
func (s *Store) MarkRulesImported() error { return s.setMeta("rules_imported", "yes") }
