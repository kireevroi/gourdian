package stats

import (
	"cmp"
	"database/sql"
	"errors"
	"fmt"
	"gourdian/internal/model"
	"slices"
	"strings"
	"time"
)

func (s *Store) AppendMatch(m model.MatchSummary) error {
	err := s.tx(func(tx *sql.Tx) error { return appendMatch(tx, m) })
	if err == nil {
		s.history.Add(1)
	}
	return err
}

// execer runs statements on the store's connection or inside a transaction.
type execer interface {
	Exec(query string, args ...any) (sql.Result, error)
	QueryRow(query string, args ...any) *sql.Row
}

func appendMatch(q execer, m model.MatchSummary) error {
	var seen int
	switch err := q.QueryRow(`SELECT 1 FROM matches WHERE match_id = ?`, m.MatchID).Scan(&seen); {
	case err == nil:
		return ErrDuplicate
	case !errors.Is(err, sql.ErrNoRows):
		return err
	}
	_, err := q.Exec(`INSERT INTO matches (`+matchColumnList+`) VALUES (`+matchPlaceholder+`)`, matchValues(m)...)
	return err
}

// UpdateMatch changes one match, for example to add OpenDota's data after the game.
func (s *Store) UpdateMatch(matchID string, update func(*model.MatchSummary)) error {
	defer s.history.Add(1)
	return s.tx(func(tx *sql.Tx) error {
		var rowID int64
		if err := tx.QueryRow(`SELECT rowid FROM matches WHERE match_id = ?`, matchID).Scan(&rowID); errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("match %s: %w", matchID, ErrNoMatch)
		} else if err != nil {
			return err
		}
		rows, err := tx.Query(`SELECT `+matchColumnList+` FROM matches WHERE match_id = ?`, matchID)
		if err != nil {
			return err
		}
		if !rows.Next() {
			rows.Close()
			return cmp.Or(rows.Err(), fmt.Errorf("match %s: %w", matchID, ErrNoMatch))
		}
		m, err := scanMatch(rows)
		rows.Close()
		if err != nil {
			return err
		}
		update(&m)
		m.MatchID = matchID
		// The row keeps its rowid, so matches stay in the order they were played.
		_, err = tx.Exec(`REPLACE INTO matches (rowid, `+matchColumnList+`) VALUES (?, `+matchPlaceholder+`)`,
			append([]any{rowID}, matchValues(m)...)...)
		return err
	})
}

func (s *Store) queryMatches(where string, args ...any) ([]model.MatchSummary, error) {
	rows, err := s.db.Query(`SELECT `+matchColumnList+` FROM matches `+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.MatchSummary
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
func (s *Store) Matches() ([]model.MatchSummary, error) {
	return s.queryMatches(`ORDER BY julianday(ended_at), rowid`)
}

// Recent returns up to n matches, newest first.
// Recent is the newest n matches, newest first, leaving out Turbo for the same reason
// MatchFilter does.
func (s *Store) Recent(n int) ([]model.MatchSummary, error) {
	return s.queryMatches(`WHERE IFNULL(game_mode, 0) != ? ORDER BY julianday(ended_at) DESC, rowid DESC LIMIT ?`,
		model.GameModeTurbo, n)
}

// Match returns one match by id.
func (s *Store) Match(matchID string) (model.MatchSummary, error) {
	found, err := s.queryMatches(`WHERE match_id = ?`, matchID)
	if err != nil {
		return model.MatchSummary{}, err
	}
	if len(found) == 0 {
		return model.MatchSummary{}, fmt.Errorf("match %s: %w", matchID, ErrNoMatch)
	}
	return found[0], nil
}

// AppendItems stores item timings, ignoring ones already known for the same match and source.
func (s *Store) AppendItems(items []model.ItemTiming) error {
	if len(items) == 0 {
		return nil
	}
	defer s.history.Add(1) // item timings are part of what targets are built from
	return s.tx(func(tx *sql.Tx) error { return appendItems(tx, items) })
}

func appendItems(q execer, items []model.ItemTiming) error {
	for _, it := range items {
		_, err := q.Exec(`INSERT OR IGNORE INTO items (match_id, hero, item, time, source) VALUES (?, ?, ?, ?, ?)`,
			it.MatchID, it.Hero, it.Item, it.Time, it.Source)
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) Items() ([]model.ItemTiming, error) {
	rows, err := s.db.Query(`SELECT match_id, hero, item, time, source FROM items ORDER BY rowid`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.ItemTiming
	for rows.Next() {
		var it model.ItemTiming
		if err := rows.Scan(&it.MatchID, &it.Hero, &it.Item, &it.Time, &it.Source); err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

func (s *Store) AppendSamples(samples []model.Sample) error {
	if len(samples) == 0 {
		return nil
	}
	return s.tx(func(tx *sql.Tx) error { return appendSamples(tx, samples) })
}

func appendSamples(q execer, samples []model.Sample) error {
	for _, x := range samples {
		_, err := q.Exec(`REPLACE INTO samples (match_id, clock, gold, gpm, xpm, last_hits, denies, kills, deaths, assists, level, alive)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			x.MatchID, x.Clock, x.Gold, x.GPM, x.XPM, x.LastHits, x.Denies, x.Kills, x.Deaths, x.Assists, x.Level, boolInt(x.Alive))
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) Timeline() ([]model.Sample, error) {
	rows, err := s.db.Query(`SELECT match_id, clock, gold, gpm, xpm, last_hits, denies, kills, deaths, assists, level, alive
		FROM samples ORDER BY match_id, clock`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Sample
	for rows.Next() {
		var x model.Sample
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

func (s *Store) AppendTips(tips []model.TipRecord) error {
	if len(tips) == 0 {
		return nil
	}
	return s.tx(func(tx *sql.Tx) error { return appendTips(tx, tips) })
}

func appendTips(q execer, tips []model.TipRecord) error {
	for _, t := range tips {
		_, err := q.Exec(`INSERT INTO tips (at, match_id, clock, rule, category, severity, habit, text)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			timeValue(t.At), t.MatchID, t.Clock, t.Rule, t.Category, t.Severity, boolInt(t.Habit), t.Text)
		if err != nil {
			return err
		}
	}
	return nil
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
func (s *Store) Tips() ([]model.TipRecord, error) { return s.tipRecords() }

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

func (s *Store) AppendMMR(e model.MMREntry) error {
	return s.tx(func(tx *sql.Tx) error { return appendMMR(tx, e) })
}

// appendMMR replaces a match's entry, so logging a match again doesn't count it twice.
func appendMMR(q execer, e model.MMREntry) error {
	if e.MatchID != "" {
		if _, err := q.Exec(`DELETE FROM mmr WHERE match_id = ?`, e.MatchID); err != nil {
			return err
		}
	}
	_, err := q.Exec(`INSERT INTO mmr (date, mmr, note, match_id) VALUES (?, ?, ?, ?)`,
		timeValue(e.Date), e.MMR, e.Note, e.MatchID)
	return err
}

func (s *Store) MMR() ([]model.MMREntry, error) {
	rows, err := s.db.Query(`SELECT date, mmr, note, match_id FROM mmr ORDER BY julianday(date), rowid`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.MMREntry
	for rows.Next() {
		var e model.MMREntry
		var date string
		if err := rows.Scan(&date, &e.MMR, &e.Note, &e.MatchID); err != nil {
			return nil, err
		}
		e.Date = parseTime(date)
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *Store) AppendReview(r model.Review) error { return appendReview(s.db, r) }

func appendReview(q execer, r model.Review) error {
	_, err := q.Exec(`INSERT INTO reviews (date, match_id, hero, hero_id, role, result, summary, strengths, improve, next_game_focus, followed_focus)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		timeValue(r.Date), r.MatchID, r.Hero, r.HeroID, r.Role, r.Result, r.Summary,
		jsonValue(r.Strengths), jsonValue(r.Improve), r.NextGameFocus, r.FollowedFocus)
	return err
}

// Reviews returns every post-match review, oldest first.
func (s *Store) Reviews() ([]model.Review, error) {
	rows, err := s.db.Query(`SELECT date, match_id, hero, hero_id, role, result, summary, strengths, improve, next_game_focus,
		COALESCE(followed_focus, '') FROM reviews ORDER BY julianday(date), rowid`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Review
	for rows.Next() {
		var r model.Review
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

func (s *Store) AppendGoals(goals []model.Goal) error {
	if len(goals) == 0 {
		return nil
	}
	return s.tx(func(tx *sql.Tx) error { return appendGoals(tx, goals) })
}

func appendGoals(q execer, goals []model.Goal) error {
	for _, g := range goals {
		_, err := q.Exec(`INSERT INTO goals (created, week, metric, comparator, target, label, match_id)
			VALUES (?, ?, ?, ?, ?, ?, ?)`,
			timeValue(g.Created), g.Week, g.Metric, g.Comparator, g.Target, g.Label, g.MatchID)
		if err != nil {
			return err
		}
	}
	return nil
}

// Goals returns every goal ever set, oldest first.
func (s *Store) Goals() ([]model.Goal, error) {
	rows, err := s.db.Query(`SELECT created, week, metric, comparator, target, label, match_id FROM goals ORDER BY julianday(created), rowid`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Goal
	for rows.Next() {
		var g model.Goal
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
// If the flag can't be read it counts as set: importing again would put an old rules file back
// over the player's rules.
func (s *Store) RulesImported() bool {
	v, err := s.meta("rules_imported")
	return err != nil || v != ""
}

// MarkRulesImported records that the old rules file has been read.
func (s *Store) MarkRulesImported() error { return setMeta(s.db, "rules_imported", "yes") }

// MatchFilter says which matches MatchesWhere returns; the zero value means all of them
// except Turbo, which has to be asked for.
type MatchFilter struct {
	HeroID int       // 0: any hero
	Role   string    // "": any position
	Since  time.Time // zero: any time
	Real   bool      // only matches the player really played, as model.MatchSummary.Real says
	Limit  int       // 0: all; otherwise only the newest this many
	// Turbo brings back the Turbo matches, which are otherwise left out. A Turbo game pays
	// about twice the gold and experience, so its numbers would drag every average, median
	// and target towards something no normal game will ever reach. Only a caller showing
	// Turbo on its own terms wants them, so they are out unless asked for, and a new caller
	// can't let them back in by forgetting.
	Turbo bool
}

// MatchesWhere returns the matches f describes, oldest first, asking the data file for only
// those instead of reading the whole history.
func (s *Store) MatchesWhere(f MatchFilter) ([]model.MatchSummary, error) {
	var where []string
	var args []any
	if f.HeroID != 0 {
		where, args = append(where, "hero_id = ?"), append(args, f.HeroID)
	}
	if f.Role != "" {
		where, args = append(where, "role = ?"), append(args, f.Role)
	}
	if !f.Since.IsZero() {
		where, args = append(where, "julianday(ended_at) >= julianday(?)"), append(args, timeValue(f.Since))
	}
	if f.Real {
		where, args = append(where, "simulated = 0 AND IFNULL(source, '') != ?"), append(args, model.SourcePractice)
	}
	if !f.Turbo {
		where, args = append(where, "IFNULL(game_mode, 0) != ?"), append(args, model.GameModeTurbo)
	}
	q := ""
	if len(where) > 0 {
		q = "WHERE " + strings.Join(where, " AND ")
	}
	if f.Limit <= 0 {
		return s.queryMatches(q+` ORDER BY julianday(ended_at), rowid`, args...)
	}
	matches, err := s.queryMatches(q+` ORDER BY julianday(ended_at) DESC, rowid DESC LIMIT ?`, append(args, f.Limit)...)
	slices.Reverse(matches)
	return matches, err
}

// ItemsIn returns the item timings of the given matches.
func (s *Store) ItemsIn(matchIDs []string) ([]model.ItemTiming, error) {
	if len(matchIDs) == 0 {
		return nil, nil
	}
	args := make([]any, len(matchIDs))
	for i, id := range matchIDs {
		args[i] = id
	}
	rows, err := s.db.Query(`SELECT match_id, hero, item, time, source FROM items WHERE match_id IN (`+placeholders(len(args))+`)`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.ItemTiming
	for rows.Next() {
		var it model.ItemTiming
		if err := rows.Scan(&it.MatchID, &it.Hero, &it.Item, &it.Time, &it.Source); err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

// UsualRole is the position the player has played most on a hero, among roles, leaving out
// simulated games; a tie goes to the one played most recently. "" when there's none.
func (s *Store) UsualRole(heroID int, roles []string) (string, error) {
	if len(roles) == 0 {
		return "", nil
	}
	args := []any{heroID}
	for _, r := range roles {
		args = append(args, r)
	}
	var role string
	err := s.db.QueryRow(`SELECT role FROM matches WHERE hero_id = ? AND simulated = 0 AND role IN (`+placeholders(len(roles))+`)
		GROUP BY role ORDER BY COUNT(*) DESC, MAX(julianday(ended_at)) DESC, MAX(rowid) DESC LIMIT 1`, args...).Scan(&role)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return role, err
}

// HistoryVersion changes whenever a match is saved or changed, so what's worked out from the
// match history can be kept until then.
func (s *Store) HistoryVersion() int64 { return s.history.Load() }
