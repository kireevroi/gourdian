package model

import (
	"reflect"
	"testing"
	"time"
)

func TestWeek(t *testing.T) {
	tests := []struct {
		name string
		t    time.Time
		want string
	}{
		{"2024 start", time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), "2024-W01"},
		{"2023 end in 2024", time.Date(2023, 12, 31, 0, 0, 0, 0, time.UTC), "2023-W52"},
		{"middle of year", time.Date(2024, 6, 15, 0, 0, 0, 0, time.UTC), "2024-W24"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Week(tt.t); got != tt.want {
				t.Errorf("Week() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMetricValue(t *testing.T) {
	m := MatchSummary{
		LastHitsAt: map[string]int{"10:00": 50},
		Deaths:     5,
		GPM:        450,
		XPM:        0, // explicitly 0
		Kills:      10,
		Assists:    15,
		TipCounts:  map[string]int{"rule1": 2},
		Source:     SourceLive,
	}

	mImported := MatchSummary{
		TipCounts: map[string]int{"rule1": 2},
		Source:    SourceOpenDota,
	}

	tests := []struct {
		name   string
		match  MatchSummary
		metric string
		wantV  float64
		wantOk bool
	}{
		{"last hits present", m, "lh_10", 50, true},
		{"last hits missing", m, "lh_15", 0, false},
		{"deaths", m, "deaths", 5, true},
		{"gpm positive", m, "gpm", 450, true},
		{"xpm zero", m, "xpm", 0, false},
		{"kills_assists", m, "kills_assists", 25, true},
		{"mistakes live", m, "mistakes_rule1", 2, true},
		{"mistakes missing rule", m, "mistakes_rule2", 0, true},
		{"mistakes imported", mImported, "mistakes_rule1", 2, false},
		{"unknown metric", m, "unknown", 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotV, gotOk := MetricValue(tt.match, tt.metric)
			if gotV != tt.wantV || gotOk != tt.wantOk {
				t.Errorf("MetricValue() = (%v, %v), want (%v, %v)", gotV, gotOk, tt.wantV, tt.wantOk)
			}
		})
	}
}

func TestGoal_Met(t *testing.T) {
	m := MatchSummary{
		Deaths: 5,
		GPM:    450,
	}

	tests := []struct {
		name    string
		goal    Goal
		match   MatchSummary
		wantMet bool
		wantOk  bool
	}{
		{"at_most met", Goal{Metric: "deaths", Comparator: "at_most", Target: 5}, m, true, true},
		{"at_most not met", Goal{Metric: "deaths", Comparator: "at_most", Target: 4}, m, false, true},
		{"at_least met", Goal{Metric: "gpm", Comparator: "at_least", Target: 400}, m, true, true},
		{"at_least not met", Goal{Metric: "gpm", Comparator: "at_least", Target: 500}, m, false, true},
		{"metric missing", Goal{Metric: "xpm", Comparator: "at_least", Target: 400}, m, false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotMet, gotOk := tt.goal.Met(tt.match)
			if gotMet != tt.wantMet || gotOk != tt.wantOk {
				t.Errorf("Goal.Met() = (%v, %v), want (%v, %v)", gotMet, gotOk, tt.wantMet, tt.wantOk)
			}
		})
	}
}

func TestWeekGoals(t *testing.T) {
	g1 := Goal{Metric: "gpm", Week: "2024-W01", Target: 400}
	g2 := Goal{Metric: "xpm", Week: "2024-W01", Target: 500}
	g3 := Goal{Metric: "gpm", Week: "2024-W01", Target: 450} // newer for same metric
	g4 := Goal{Metric: "deaths", Week: "2024-W02", Target: 5} // different week

	goals := []Goal{g1, g2, g3, g4}

	want := []Goal{g3, g2} // g3 replaced g1, g2 kept

	got := WeekGoals(goals, "2024-W01")

	if !reflect.DeepEqual(got, want) {
		t.Errorf("WeekGoals() = %v, want %v", got, want)
	}
}

func TestProgress(t *testing.T) {
	created := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	week := Week(created)

	g1 := Goal{Created: created, Week: week, Metric: "gpm", Comparator: "at_least", Target: 400, MatchID: "match_goal_1"}
	g2 := Goal{Created: created, Week: week, Metric: "deaths", Comparator: "at_most", Target: 5, MatchID: "match_goal_2"}

	goals := []Goal{g1, g2}

	matches := []MatchSummary{
		{MatchID: "sim", Simulated: true}, // Ignored
		{MatchID: "past", EndedAt: created.Add(-time.Hour), Source: SourceOpenDota}, // Ignored
		{MatchID: "wrong_week", EndedAt: created.AddDate(0, 0, 14), Source: SourceOpenDota}, // Ignored
		{MatchID: "match_goal_1", EndedAt: created.Add(time.Hour), Source: SourceOpenDota, Deaths: 6}, // Ignored by g1, met by g2? No, g2 not met (6 > 5) but tried
		{MatchID: "match_goal_2", EndedAt: created.Add(time.Hour), Source: SourceOpenDota, GPM: 450}, // Ignored by g2, met by g1 (450 > 400)

		// Valid matches
		{MatchID: "m1", EndedAt: created.Add(time.Hour), Source: SourceOpenDota, GPM: 450, Deaths: 6}, // g1 met, g2 not met
		{MatchID: "m2", EndedAt: created.Add(2 * time.Hour), Source: SourceOpenDota, GPM: 0, Deaths: 4}, // g1 ignored (ok=false), g2 met
		{MatchID: "m3", EndedAt: created.Add(3 * time.Hour), Source: SourceOpenDota, GPM: 300, Deaths: 4}, // g1 not met, g2 met
		{MatchID: "m4", EndedAt: created.Add(4 * time.Hour), Source: SourceOpenDota, Deaths: 4}, // g1 ignored (ok=false), g2 met
		{MatchID: "m5", EndedAt: created.Add(5 * time.Hour), Source: SourceOpenDota, Deaths: 4}, // g1 ignored (ok=false), g2 met
		{MatchID: "m6", EndedAt: created.Add(6 * time.Hour), Source: SourceOpenDota, Deaths: 4}, // g1 ignored (ok=false), g2 met
	}

	got := Progress(goals, matches)

	want := []GoalProgress{
		{
			Goal:    g1,
			Met:     2, // match_goal_2, m1
			Tried:   3, // match_goal_2, m1, m3 (m2, m4, m5, m6 didn't have GPM)
			Done:    false,
			Streak:  0, // m3 broke streak
			LastMet: false,
		},
		{
			Goal:    g2,
			Met:     5, // m2, m3, m4, m5, m6
			Tried:   7, // match_goal_1, m1, m2, m3, m4, m5, m6
			Done:    true,
			Streak:  5,
			LastMet: true,
		},
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("Progress() = %+v, want %+v", got, want)
	}
}
