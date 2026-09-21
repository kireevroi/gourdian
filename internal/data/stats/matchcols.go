package stats

import (
	"fmt"
	"gourdian/internal/game/model"
	"strings"
	"time"
)

// matchCol is a column of the matches table: how a match's field is stored, and where the
// stored value is read back into.
type matchCol struct {
	name, typ string
	value     func(m *model.MatchSummary) any
	dest      func(m *model.MatchSummary) any
}

// matchCols are the matches table's columns, in order. The table's schema, inserts and reads
// all come from here, so a new field is one line here plus a migration adding its column to
// older files.
var matchCols = []matchCol{
	{"match_id", "TEXT PRIMARY KEY", func(m *model.MatchSummary) any { return m.MatchID }, func(m *model.MatchSummary) any { return &m.MatchID }},
	{"ended_at", "TEXT", func(m *model.MatchSummary) any { return timeValue(m.EndedAt) }, func(m *model.MatchSummary) any { return timeCol{&m.EndedAt} }},
	{"source", "TEXT", func(m *model.MatchSummary) any { return m.Source }, func(m *model.MatchSummary) any { return &m.Source }},
	{"hero_id", "INTEGER", func(m *model.MatchSummary) any { return m.HeroID }, func(m *model.MatchSummary) any { return &m.HeroID }},
	{"hero", "TEXT", func(m *model.MatchSummary) any { return m.Hero }, func(m *model.MatchSummary) any { return &m.Hero }},
	{"role", "TEXT", func(m *model.MatchSummary) any { return m.Role }, func(m *model.MatchSummary) any { return &m.Role }},
	{"team", "TEXT", func(m *model.MatchSummary) any { return m.Team }, func(m *model.MatchSummary) any { return &m.Team }},
	{"result", "TEXT", func(m *model.MatchSummary) any { return m.Result }, func(m *model.MatchSummary) any { return &m.Result }},
	{"duration_sec", "INTEGER", func(m *model.MatchSummary) any { return m.DurationSec }, func(m *model.MatchSummary) any { return &m.DurationSec }},
	{"kills", "INTEGER", func(m *model.MatchSummary) any { return m.Kills }, func(m *model.MatchSummary) any { return &m.Kills }},
	{"deaths", "INTEGER", func(m *model.MatchSummary) any { return m.Deaths }, func(m *model.MatchSummary) any { return &m.Deaths }},
	{"assists", "INTEGER", func(m *model.MatchSummary) any { return m.Assists }, func(m *model.MatchSummary) any { return &m.Assists }},
	{"last_hits", "INTEGER", func(m *model.MatchSummary) any { return m.LastHits }, func(m *model.MatchSummary) any { return &m.LastHits }},
	{"denies", "INTEGER", func(m *model.MatchSummary) any { return m.Denies }, func(m *model.MatchSummary) any { return &m.Denies }},
	{"gpm", "INTEGER", func(m *model.MatchSummary) any { return m.GPM }, func(m *model.MatchSummary) any { return &m.GPM }},
	{"xpm", "INTEGER", func(m *model.MatchSummary) any { return m.XPM }, func(m *model.MatchSummary) any { return &m.XPM }},
	{"rank_tier", "INTEGER", func(m *model.MatchSummary) any { return m.RankTier }, func(m *model.MatchSummary) any { return &m.RankTier }},
	{"game_mode", "INTEGER", func(m *model.MatchSummary) any { return m.GameMode }, func(m *model.MatchSummary) any { return &m.GameMode }},
	{"simulated", "INTEGER", func(m *model.MatchSummary) any { return boolInt(m.Simulated) }, func(m *model.MatchSummary) any { return boolCol{&m.Simulated} }},
	{"ranked", "INTEGER", func(m *model.MatchSummary) any { return boolInt(m.Ranked) }, func(m *model.MatchSummary) any { return boolCol{&m.Ranked} }},
	{"parsed", "INTEGER", func(m *model.MatchSummary) any { return boolInt(m.Parsed) }, func(m *model.MatchSummary) any { return boolCol{&m.Parsed} }},
	{"lane_role", "INTEGER", func(m *model.MatchSummary) any { return m.LaneRole }, func(m *model.MatchSummary) any { return &m.LaneRole }},
	{"net_worth", "INTEGER", func(m *model.MatchSummary) any { return m.NetWorth }, func(m *model.MatchSummary) any { return &m.NetWorth }},
	{"hero_damage", "INTEGER", func(m *model.MatchSummary) any { return m.HeroDamage }, func(m *model.MatchSummary) any { return &m.HeroDamage }},
	{"tower_damage", "INTEGER", func(m *model.MatchSummary) any { return m.TowerDamage }, func(m *model.MatchSummary) any { return &m.TowerDamage }},
	{"obs_placed", "INTEGER", func(m *model.MatchSummary) any { return m.ObsPlaced }, func(m *model.MatchSummary) any { return &m.ObsPlaced }},
	{"sen_placed", "INTEGER", func(m *model.MatchSummary) any { return m.SenPlaced }, func(m *model.MatchSummary) any { return &m.SenPlaced }},
	{"camps_stacked", "INTEGER", func(m *model.MatchSummary) any { return m.CampsStacked }, func(m *model.MatchSummary) any { return &m.CampsStacked }},
	{"teamfight", "REAL", func(m *model.MatchSummary) any { return m.TeamfightParticipation }, func(m *model.MatchSummary) any { return &m.TeamfightParticipation }},
	{"gpm_pct", "REAL", func(m *model.MatchSummary) any { return m.GPMPct }, func(m *model.MatchSummary) any { return &m.GPMPct }},
	{"lh_pct", "REAL", func(m *model.MatchSummary) any { return m.LHPct }, func(m *model.MatchSummary) any { return &m.LHPct }},
	{"hero_damage_pct", "REAL", func(m *model.MatchSummary) any { return m.HeroDamagePct }, func(m *model.MatchSummary) any { return &m.HeroDamagePct }},
	{"enemy_heroes", "TEXT", func(m *model.MatchSummary) any { return jsonValue(m.EnemyHeroes) }, func(m *model.MatchSummary) any { return stringsCol{&m.EnemyHeroes} }},
	{"last_hits_at", "TEXT", func(m *model.MatchSummary) any { return jsonValue(m.LastHitsAt) }, func(m *model.MatchSummary) any { return countsCol{&m.LastHitsAt} }},
	{"death_clocks", "TEXT", func(m *model.MatchSummary) any { return jsonValue(m.DeathClocks) }, func(m *model.MatchSummary) any { return intsCol{&m.DeathClocks} }},
	{"tip_counts", "TEXT", func(m *model.MatchSummary) any { return jsonValue(m.TipCounts) }, func(m *model.MatchSummary) any { return countsCol{&m.TipCounts} }},
}

var (
	matchColumnList  = joinCols(func(c matchCol) string { return c.name })
	matchTable       = "CREATE TABLE IF NOT EXISTS matches (" + joinCols(func(c matchCol) string { return c.name + " " + c.typ }) + ");"
	matchPlaceholder = placeholders(len(matchCols))
)

func joinCols(f func(matchCol) string) string {
	parts := make([]string, len(matchCols))
	for i, c := range matchCols {
		parts[i] = f(c)
	}
	return strings.Join(parts, ", ")
}

func matchValues(m model.MatchSummary) []any {
	out := make([]any, len(matchCols))
	for i, c := range matchCols {
		out[i] = c.value(&m)
	}
	return out
}

// scanner is *sql.Rows or *sql.Row.
type scanner interface{ Scan(dest ...any) error }

func scanMatch(rows scanner) (model.MatchSummary, error) {
	var m model.MatchSummary
	dest := make([]any, len(matchCols))
	for i, c := range matchCols {
		dest[i] = c.dest(&m)
	}
	if err := rows.Scan(dest...); err != nil {
		return m, err
	}
	if m.Source == "" {
		m.Source = model.SourceLive
		if m.Simulated {
			m.Source = model.SourceSim
		}
	}
	return m, nil
}

// Columns stored in another form than their field read back through these.
type (
	timeCol    struct{ p *time.Time }
	boolCol    struct{ p *bool }
	stringsCol struct{ p *[]string }
	countsCol  struct{ p *map[string]int }
	intsCol    struct{ p *[]int }
)

func (c boolCol) Scan(src any) error {
	n, ok := src.(int64)
	if !ok && src != nil {
		return fmt.Errorf("bool column holds %T", src)
	}
	*c.p = n != 0
	return nil
}

func (c timeCol) Scan(src any) error    { *c.p = parseTime(text(src)); return nil }
func (c stringsCol) Scan(src any) error { *c.p = stringsFromJSON(text(src)); return nil }
func (c countsCol) Scan(src any) error  { *c.p = countsFromJSON(text(src)); return nil }
func (c intsCol) Scan(src any) error    { *c.p = intsFromJSON(text(src)); return nil }

// text reads a TEXT column, where NULL is "".
func text(src any) string {
	switch v := src.(type) {
	case string:
		return v
	case []byte:
		return string(v)
	}
	return ""
}
