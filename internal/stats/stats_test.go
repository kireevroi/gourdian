package stats

import (
	"database/sql"
	"encoding/csv"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

func TestMatchRoundTrip(t *testing.T) {
	st := openTest(t, t.TempDir())
	want := MatchSummary{
		MatchID: "8001", HeroID: 1, Hero: "Anti-Mage", Role: "carry", Team: "radiant", Result: "win",
		EndedAt: time.Date(2026, 9, 17, 20, 0, 0, 0, time.UTC), DurationSec: 2400, Kills: 7, Deaths: 3, Assists: 9,
		LastHits: 310, Denies: 12, GPM: 610, XPM: 700, RankTier: 43,
		LastHitsAt:  map[string]int{"5:00": 31, "10:00": 70},
		DeathClocks: []int{490, 1320},
		TipCounts:   map[string]int{"no_tp": 2, "stash": 1},
	}
	if err := st.AppendMatch(want); err != nil {
		t.Fatal(err)
	}
	got, err := st.Matches()
	if err != nil || len(got) != 1 {
		t.Fatalf("Matches() = %v, %v", got, err)
	}
	m := got[0]
	if m.MatchID != want.MatchID || m.GPM != 610 || m.RankTier != 43 || !m.EndedAt.Equal(want.EndedAt) ||
		m.LastHitsAt["10:00"] != 70 || !slices.Equal(m.DeathClocks, want.DeathClocks) || m.TipCounts["no_tp"] != 2 {
		t.Fatalf("round trip mismatch: %+v", m)
	}
}

func TestAppendMatchSkipsDuplicates(t *testing.T) {
	st := openTest(t, t.TempDir())
	if err := st.AppendMatch(MatchSummary{MatchID: "42"}); err != nil {
		t.Fatal(err)
	}
	if err := st.AppendMatch(MatchSummary{MatchID: "42"}); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("second append: %v", err)
	}
	if got, _ := st.Matches(); len(got) != 1 {
		t.Fatalf("got %d matches", len(got))
	}
}

func TestExportWritesSpreadsheetFiles(t *testing.T) {
	st := openTest(t, t.TempDir())
	st.AppendMatch(MatchSummary{MatchID: "1", TipCounts: map[string]int{"no_tp": 1}})
	st.AppendMatch(MatchSummary{MatchID: "2", TipCounts: map[string]int{"wards": 4}})
	if _, err := st.Export(); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(filepath.Join(st.Dir(), MatchesFile))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	records, err := csv.NewReader(f).ReadAll()
	if err != nil {
		t.Fatalf("the export is not valid CSV: %v", err)
	}
	header := records[0]
	if !slices.Contains(header, "mistakes_no_tp") || !slices.Contains(header, "mistakes_wards") || len(records) != 3 {
		t.Fatalf("header %v with %d rows", header, len(records)-1)
	}
	got, _ := st.Matches()
	if got[0].TipCounts["no_tp"] != 1 || got[0].TipCounts["wards"] != 0 || got[1].TipCounts["wards"] != 4 {
		t.Fatalf("counts: %+v / %+v", got[0].TipCounts, got[1].TipCounts)
	}
}

func TestUpdateMatchAddsParsedData(t *testing.T) {
	st := openTest(t, t.TempDir())
	st.AppendMatch(MatchSummary{MatchID: "1", Hero: "Lina", GPM: 400, TipCounts: map[string]int{"no_tp": 2}})
	st.AppendMatch(MatchSummary{MatchID: "2", Hero: "Axe"})
	err := st.UpdateMatch("1", func(m *MatchSummary) {
		m.Parsed, m.Source, m.LaneRole, m.NetWorth, m.GPMPct = true, SourceOpenDota, 2, 15300, 0.42
		m.EnemyHeroes = []string{"Pudge", "Sniper"}
		m.LastHitsAt["10:00"] = 51
	})
	if err != nil {
		t.Fatal(err)
	}
	got, _ := st.Matches()
	m := got[0]
	if !m.Parsed || m.LaneRole != 2 || m.NetWorth != 15300 || m.GPMPct != 0.42 || m.LastHitsAt["10:00"] != 51 ||
		!slices.Equal(m.EnemyHeroes, []string{"Pudge", "Sniper"}) || m.TipCounts["no_tp"] != 2 || m.GPM != 400 {
		t.Fatalf("updated match = %+v", m)
	}
	if got[1].Parsed || got[1].Hero != "Axe" || got[1].Source != SourceLive {
		t.Fatalf("other match changed: %+v", got[1])
	}
	if err := st.UpdateMatch("404", func(*MatchSummary) {}); err == nil {
		t.Fatal("updating an unknown match should fail")
	}
}

func TestItemsRoundTrip(t *testing.T) {
	st := openTest(t, t.TempDir())
	want := []ItemTiming{{MatchID: "1", Hero: "Lina", Item: "blink", Time: 840, Source: "opendota"}}
	if err := st.AppendItems(want); err != nil {
		t.Fatal(err)
	}
	if got, err := st.Items(); err != nil || !slices.Equal(got, want) {
		t.Fatalf("items = %+v, %v", got, err)
	}
	gsi := ItemTiming{MatchID: "1", Hero: "Lina", Item: "blink", Time: 845, Source: "gsi"}
	st.AppendItems(append(want, gsi))
	if got, _ := st.Items(); len(got) != 2 || got[1] != gsi {
		t.Fatalf("re-appending should add only the new source: %+v", got)
	}
}

func TestRecentIsNewestFirst(t *testing.T) {
	st := openTest(t, t.TempDir())
	for _, id := range []string{"a", "b", "c"} {
		st.AppendMatch(MatchSummary{MatchID: id})
	}
	got, _ := st.Recent(2)
	if len(got) != 2 || got[0].MatchID != "c" || got[1].MatchID != "b" {
		t.Fatalf("Recent(2) = %+v", got)
	}
}

func TestTimelineTipsAndMMR(t *testing.T) {
	st := openTest(t, t.TempDir())
	st.AppendSamples([]Sample{{MatchID: "1", Clock: 60, LastHits: 8, Alive: true}, {MatchID: "1", Clock: 120, LastHits: 17}})
	st.AppendTips([]TipRecord{{At: time.Now(), MatchID: "1", Clock: 90, Rule: "no_tp", Habit: true, Text: `Buy a "TP", now`}})
	st.AppendMMR(MMREntry{Date: time.Now(), MMR: 2450, Note: "after placement"})

	timeline, _ := st.Timeline()
	if len(timeline) != 2 || timeline[1].LastHits != 17 || !timeline[0].Alive {
		t.Fatalf("timeline = %+v", timeline)
	}
	mmr, _ := st.MMR()
	if len(mmr) != 1 || mmr[0].MMR != 2450 || mmr[0].Note != "after placement" {
		t.Fatalf("mmr = %+v", mmr)
	}
	tips, err := st.Tips()
	if err != nil || len(tips) != 1 || tips[0].Text != `Buy a "TP", now` || !tips[0].Habit {
		t.Fatalf("tips = %+v, %v", tips, err)
	}
}

func TestReviewRoundTrip(t *testing.T) {
	st := openTest(t, t.TempDir())
	want := Review{Date: time.Date(2026, 9, 17, 21, 0, 0, 0, time.UTC), MatchID: "9", Hero: "Lina", Result: "loss",
		Summary: "Strong lane, then 6 deaths, all\nwithout vision.", Strengths: []string{"Lane", "Rune control"},
		Improve: []string{"Ward before pushing", "Carry a TP"}, NextGameFocus: "Die at most 5 times"}
	if err := st.AppendReview(want); err != nil {
		t.Fatal(err)
	}
	got, err := st.Reviews()
	if err != nil || len(got) != 1 {
		t.Fatalf("Reviews() = %v, %v", got, err)
	}
	r := got[0]
	if r.Summary != want.Summary || !slices.Equal(r.Strengths, want.Strengths) || !slices.Equal(r.Improve, want.Improve) ||
		r.NextGameFocus != want.NextGameFocus || !r.Date.Equal(want.Date) {
		t.Fatalf("round trip mismatch: %+v", r)
	}
}

func TestGoalProgress(t *testing.T) {
	st := openTest(t, t.TempDir())
	monday := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	week := Week(monday)
	st.AppendGoals([]Goal{
		{Created: monday, Week: week, Metric: "lh_10", Comparator: "at_least", Target: 50, Label: "50 LH at 10:00", MatchID: "r1"},
		{Created: monday, Week: week, Metric: "deaths", Comparator: "at_most", Target: 6, Label: "6 deaths or fewer"},
		{Created: monday.Add(time.Hour), Week: week, Metric: "lh_10", Comparator: "at_least", Target: 55, Label: "55 LH at 10:00"},
	})
	all, _ := st.Goals()
	goals := WeekGoals(all, week)
	if len(goals) != 2 || goals[0].Target != 55 {
		t.Fatalf("goals = %+v", goals)
	}
	match := func(id string, hours int, lh, deaths int, source string) MatchSummary {
		return MatchSummary{MatchID: id, EndedAt: monday.Add(time.Duration(hours) * time.Hour), Source: source,
			Deaths: deaths, LastHitsAt: map[string]int{"10:00": lh}}
	}
	matches := []MatchSummary{
		match("before", -1, 70, 2, SourceLive),
		match("a", 2, 56, 8, SourceLive),
		match("b", 3, 40, 3, SourceOpenDota),
		match("c", 4, 60, 5, SourcePractice),
		match("d", 5, 58, 4, SourceLive),
		match("next-week", 24*8, 90, 0, SourceLive),
	}
	p := Progress(goals, matches)
	if p[0].Met != 2 || p[0].Tried != 3 || p[0].Streak != 1 || !p[0].LastMet || p[1].Met != 2 || p[1].Streak != 2 {
		t.Fatalf("progress = %+v", p)
	}
}

func openTest(t *testing.T, dir string) *Store {
	t.Helper()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestMMRForAMatchReplacesTheEarlierReading(t *testing.T) {
	st := openTest(t, t.TempDir())
	st.AppendMMR(MMREntry{Date: time.Now(), MMR: 3000})
	st.AppendMMR(MMREntry{Date: time.Now(), MMR: 3025, MatchID: "1", Note: "win"})
	st.AppendMMR(MMREntry{Date: time.Now(), MMR: 3030, MatchID: "1", Note: "win"})
	got, err := st.MMR()
	if err != nil || len(got) != 2 {
		t.Fatalf("mmr = %+v, %v", got, err)
	}
	if got[1].MMR != 3030 || got[1].MatchID != "1" {
		t.Fatalf("the corrected reading should be the only one for that match: %+v", got)
	}
}

func TestEntriesSortByTimeAcrossTimeZones(t *testing.T) {
	s := openTest(t, t.TempDir())
	tbilisi, brazil := time.FixedZone("GET", 4*3600), time.FixedZone("BRT", -3*3600)
	first := time.Date(2026, 9, 18, 10, 0, 0, 0, tbilisi) // 06:00 UTC
	second := time.Date(2026, 9, 18, 5, 0, 0, 0, brazil)  // 08:00 UTC
	for _, e := range []MMREntry{{Date: second, MMR: 3025}, {Date: first, MMR: 3000}} {
		if err := s.AppendMMR(e); err != nil {
			t.Fatal(err)
		}
	}
	for _, m := range []MatchSummary{{MatchID: "2", EndedAt: second}, {MatchID: "1", EndedAt: first}} {
		if err := s.AppendMatch(m); err != nil {
			t.Fatal(err)
		}
	}
	if entries, _ := s.MMR(); len(entries) != 2 || entries[1].MMR != 3025 {
		t.Fatalf("mmr out of order: %+v", entries)
	}
	if recent, _ := s.Recent(1); len(recent) != 1 || recent[0].MatchID != "2" {
		t.Fatalf("newest match = %+v", recent)
	}
}

// A failed import of an older version's CSV files must leave nothing behind and run again on
// the next start. It used to mark itself done first, so everything after the failure was lost.
func TestCSVImportIsAllOrNothing(t *testing.T) {
	dir := t.TempDir()
	statsDir := filepath.Join(dir, "stats")
	if err := os.MkdirAll(filepath.Join(statsDir, GoalsFile), 0o755); err != nil { // unreadable: a folder
		t.Fatal(err)
	}
	write := func(name, content string) {
		if err := os.WriteFile(filepath.Join(statsDir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(TipsFile, "at,match_id,clock,rule,category,severity,habit,text\n"+
		"2026-01-01T00:00:00Z,m1,60,no_tp,items,warn,true,No TP\n2026-01-01T00:01:00Z,m1,120,no_tp,items,warn,true,No TP\n")
	counts := func() (tips, goals int) {
		st, err := Open(dir)
		if err != nil {
			t.Fatal(err)
		}
		defer st.Close()
		tt, err := st.Tips()
		if err != nil {
			t.Fatal(err)
		}
		gg, err := st.Goals()
		if err != nil {
			t.Fatal(err)
		}
		return len(tt), len(gg)
	}
	if tips, _ := counts(); tips != 0 {
		t.Fatalf("the failed import left %d tips behind", tips)
	}
	if err := os.Remove(filepath.Join(statsDir, GoalsFile)); err != nil {
		t.Fatal(err)
	}
	write(GoalsFile, "created,week,metric,comparator,target,label,match_id\n2026-01-01T00:00:00Z,2026-W01,deaths,<=,5,Die less,m1\n")
	for range 2 { // the retry imports everything once; later starts import nothing
		if tips, goals := counts(); tips != 2 || goals != 1 {
			t.Fatalf("%d tips (want 2) and %d goals (want 1) after the retry", tips, goals)
		}
	}
}

// A data file from an older version gets the columns and indexes it lacks, once.
func TestOpenUpgradesAnOlderFile(t *testing.T) {
	dir := t.TempDir()
	old, err := sql.Open("sqlite", "file:"+filepath.Join(dir, DataFile))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := old.Exec(`CREATE TABLE reviews (date TEXT, match_id TEXT, hero TEXT, hero_id INTEGER, role TEXT, result TEXT,
		summary TEXT, strengths TEXT, improve TEXT, next_game_focus TEXT)`); err != nil { // before 1.2: no followed_focus
		t.Fatal(err)
	}
	old.Close()
	for range 2 {
		st, err := Open(dir)
		if err != nil {
			t.Fatal(err)
		}
		var version, hasColumn, hasIndex int
		st.db.QueryRow(`PRAGMA user_version`).Scan(&version)
		st.db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('reviews') WHERE name = 'followed_focus'`).Scan(&hasColumn)
		st.db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name = 'matches_hero_role'`).Scan(&hasIndex)
		st.Close()
		if version != len(migrations) || hasColumn != 1 || hasIndex != 1 {
			t.Fatalf("version %d of %d, followed_focus %d, index %d", version, len(migrations), hasColumn, hasIndex)
		}
	}
}
