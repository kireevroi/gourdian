package stats

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// DataFile is the trainer's own data file, holding matches, tips, reviews and rules.
const DataFile = "trainer.data"

const schema = `
CREATE TABLE IF NOT EXISTS matches (
  match_id TEXT PRIMARY KEY, ended_at TEXT, source TEXT, hero_id INTEGER, hero TEXT, role TEXT, team TEXT,
  result TEXT, duration_sec INTEGER, kills INTEGER, deaths INTEGER, assists INTEGER, last_hits INTEGER,
  denies INTEGER, gpm INTEGER, xpm INTEGER, rank_tier INTEGER, simulated INTEGER, ranked INTEGER,
  parsed INTEGER, lane_role INTEGER, net_worth INTEGER, hero_damage INTEGER, tower_damage INTEGER,
  obs_placed INTEGER, sen_placed INTEGER, camps_stacked INTEGER, teamfight REAL, gpm_pct REAL, lh_pct REAL,
  hero_damage_pct REAL, enemy_heroes TEXT, last_hits_at TEXT, death_clocks TEXT, tip_counts TEXT);
CREATE INDEX IF NOT EXISTS matches_ended ON matches(ended_at);

CREATE TABLE IF NOT EXISTS samples (
  match_id TEXT, clock INTEGER, gold INTEGER, gpm INTEGER, xpm INTEGER, last_hits INTEGER, denies INTEGER,
  kills INTEGER, deaths INTEGER, assists INTEGER, level INTEGER, alive INTEGER,
  PRIMARY KEY (match_id, clock)) WITHOUT ROWID;

CREATE TABLE IF NOT EXISTS tips (
  at TEXT, match_id TEXT, clock INTEGER, rule TEXT, category TEXT, severity TEXT, habit INTEGER, text TEXT);
CREATE INDEX IF NOT EXISTS tips_match ON tips(match_id);
CREATE INDEX IF NOT EXISTS tips_rule ON tips(rule);

CREATE TABLE IF NOT EXISTS items (
  match_id TEXT, hero TEXT, item TEXT, time INTEGER, source TEXT,
  UNIQUE (match_id, item, source));

CREATE TABLE IF NOT EXISTS mmr (date TEXT, mmr INTEGER, note TEXT, match_id TEXT);
CREATE INDEX IF NOT EXISTS mmr_match ON mmr(match_id);

CREATE TABLE IF NOT EXISTS reviews (
  date TEXT, match_id TEXT, hero TEXT, hero_id INTEGER, role TEXT, result TEXT, summary TEXT,
  strengths TEXT, improve TEXT, next_game_focus TEXT, followed_focus TEXT);

CREATE TABLE IF NOT EXISTS goals (
  created TEXT, week TEXT, metric TEXT, comparator TEXT, target REAL, label TEXT, match_id TEXT);

CREATE TABLE IF NOT EXISTS rules (id TEXT PRIMARY KEY, kind TEXT, spec TEXT);

CREATE TABLE IF NOT EXISTS meta (key TEXT PRIMARY KEY, value TEXT);
`

// Open prepares the trainer's data file, creating it and taking over any older CSV files
// the first time.
func Open(configDir string) (*Store, error) {
	dir := filepath.Join(configDir, "stats")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	path := filepath.Join(configDir, DataFile)
	// _txlock=immediate keeps two writers from deadlocking mid-transaction.
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_txlock=immediate")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("prepare %s: %w", DataFile, err)
	}
	if err := addColumns(db); err != nil {
		db.Close()
		return nil, err
	}
	s := &Store{dir: dir, db: db}
	if err := s.importCSV(); err != nil {
		s.Log("couldn't take over the old CSV files", err)
	}
	return s, nil
}

// addColumns adds columns that newer versions store, to a file made by an older one.
func addColumns(db *sql.DB) error {
	for _, stmt := range []string{
		`ALTER TABLE reviews ADD COLUMN followed_focus TEXT`,
	} {
		// SQLite has no "add column if missing"; the error when it is already there is fine.
		if _, err := db.Exec(stmt); err != nil && !strings.Contains(err.Error(), "duplicate column") {
			return err
		}
	}
	return nil
}

func (s *Store) Close() error { return s.db.Close() }

// Log reports a problem that doesn't stop the trainer; set by the caller.
func (s *Store) Log(msg string, err error) {
	if s.onError != nil {
		s.onError(msg, err)
	}
}

// OnError sets where problems that don't stop the trainer are reported.
func (s *Store) OnError(f func(msg string, err error)) { s.onError = f }

// meta reads a flag the store keeps about itself; a missing one is "".
func (s *Store) meta(key string) (string, error) {
	var v string
	err := s.db.QueryRow(`SELECT value FROM meta WHERE key = ?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return v, err
}

func setMeta(q execer, key, value string) error {
	_, err := q.Exec(`INSERT INTO meta (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}

func jsonValue(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(data)
}

func intsFromJSON(s string) []int {
	var out []int
	if s != "" && s != "null" {
		json.Unmarshal([]byte(s), &out)
	}
	return out
}

func stringsFromJSON(s string) []string {
	var out []string
	if s != "" && s != "null" {
		json.Unmarshal([]byte(s), &out)
	}
	return out
}

// A stored "null" unmarshals a map back to nil, so the result is always usable.
func countsFromJSON(s string) map[string]int {
	out := map[string]int{}
	if s != "" && s != "null" {
		json.Unmarshal([]byte(s), &out)
	}
	if out == nil {
		out = map[string]int{}
	}
	return out
}

func timeValue(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339)
}

func parseTime(s string) time.Time {
	t, _ := time.Parse(time.RFC3339, s)
	return t
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// tx runs f in a transaction, rolling it back on any error.
func (s *Store) tx(f func(*sql.Tx) error) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	if err := f(tx); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

func placeholders(n int) string {
	return strings.TrimSuffix(strings.Repeat("?,", n), ",")
}
