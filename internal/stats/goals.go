package stats

import (
	"fmt"
	"slices"
	"strings"
	"time"
)

// Goal is a measurable target a match review sets for the rest of the week.
type Goal struct {
	Created    time.Time `json:"created"`
	Week       string    `json:"week"`       // ISO week, like "2026-W38"
	Metric     string    `json:"metric"`     // see GoalMetrics
	Comparator string    `json:"comparator"` // "at_least" or "at_most"
	Target     float64   `json:"target"`
	Label      string    `json:"label"`
	MatchID    string    `json:"match_id"`
}

// GoalsDone is how many matches meeting a goal complete it for the week.
const GoalsDone = 5

// GoalMetrics describes the fixed metrics; "mistakes_<rule id>" counts a habit's warnings.
var GoalMetrics = map[string]string{
	"lh_5": "last hits at 5:00", "lh_10": "last hits at 10:00", "lh_15": "last hits at 15:00", "lh_20": "last hits at 20:00",
	"deaths": "deaths in the match", "gpm": "gold per minute", "xpm": "experience per minute",
	"kills_assists": "kills plus assists",
}

// Week names the ISO week a time falls in.
func Week(t time.Time) string {
	y, w := t.ISOWeek()
	return fmt.Sprintf("%d-W%02d", y, w)
}

// MetricValue reads a goal's metric from a match; false when the match didn't record it.
func MetricValue(m MatchSummary, metric string) (float64, bool) {
	switch {
	case strings.HasPrefix(metric, "lh_"):
		v, ok := m.LastHitsAt[strings.TrimPrefix(metric, "lh_")+":00"]
		return float64(v), ok
	case metric == "deaths":
		return float64(m.Deaths), true
	case metric == "gpm":
		return float64(m.GPM), m.GPM > 0
	case metric == "xpm":
		return float64(m.XPM), m.XPM > 0
	case metric == "kills_assists":
		return float64(m.Kills + m.Assists), true
	case strings.HasPrefix(metric, "mistakes_"):
		// Only live matches have warnings; imported ones would always count as clean.
		return float64(m.TipCounts[strings.TrimPrefix(metric, "mistakes_")]), m.Source == SourceLive
	}
	return 0, false
}

// Met reports whether a match meets the goal; ok is false when the match can't say.
func (g Goal) Met(m MatchSummary) (met, ok bool) {
	v, ok := MetricValue(m, g.Metric)
	if !ok {
		return false, false
	}
	if g.Comparator == "at_most" {
		return v <= g.Target, true
	}
	return v >= g.Target, true
}

var goalColumns = []string{"created", "week", "metric", "comparator", "target", "label", "match_id"}

// WeekGoals keeps a week's goals, the newest per metric, in the order they were set.
func WeekGoals(all []Goal, week string) []Goal {
	var out []Goal
	for _, g := range all {
		if g.Week != week {
			continue
		}
		if i := slices.IndexFunc(out, func(o Goal) bool { return o.Metric == g.Metric }); i >= 0 {
			out[i] = g
		} else {
			out = append(out, g)
		}
	}
	return out
}

type GoalProgress struct {
	Goal
	Met     int  `json:"met"`      // matches this week meeting it, since it was set
	Tried   int  `json:"tried"`    // matches this week that could be measured
	Done    bool `json:"done"`     // met in GoalsDone matches
	Streak  int  `json:"streak"`   // latest matches in a row meeting it
	LastMet bool `json:"last_met"` // whether the latest measured match met it
}

// Progress counts, for each goal, the real matches since it was set that meet it.
func Progress(goals []Goal, matches []MatchSummary) []GoalProgress {
	out := make([]GoalProgress, 0, len(goals))
	for _, g := range goals {
		p := GoalProgress{Goal: g}
		for _, m := range matches {
			if !m.Real() || m.EndedAt.Before(g.Created) || Week(m.EndedAt) != g.Week || m.MatchID == g.MatchID {
				continue
			}
			met, ok := g.Met(m)
			if !ok {
				continue
			}
			p.Tried++
			p.LastMet = met
			if met {
				p.Met++
				p.Streak++
			} else {
				p.Streak = 0
			}
		}
		p.Done = p.Met >= GoalsDone
		out = append(out, p)
	}
	return out
}
