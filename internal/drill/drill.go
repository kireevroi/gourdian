// Package drill scores the habit a player is working on: how often its alert fired in recent
// matches, how that compares with their usual, and which habit to suggest if they picked none.
package drill

import (
	"cmp"
	"slices"
	"time"

	"gourdian/internal/coach"
	"gourdian/internal/model"
)

const (
	// Matches is how many recent matches the drill is scored over.
	Matches = 10
	// Window is how many to read to find them, since practice and imported games drop out.
	Window = Matches * 3
)

// View is the habit the player is working on: what it is, how it went in recent matches, and
// which habit the trainer would pick if they haven't chosen one.
type View struct {
	Rule            string   `json:"rule,omitempty"`
	Label           string   `json:"label,omitempty"`
	Advice          string   `json:"advice,omitempty"`
	Average         float64  `json:"average"`
	Best            int      `json:"best"`
	Recent          []Match  `json:"recent,omitempty"`
	Suggestion      string   `json:"suggestion,omitempty"`
	SuggestionLabel string   `json:"suggestion_label,omitempty"`
	Choices         []Choice `json:"choices"`
}

type Match struct {
	MatchID string    `json:"match_id"`
	Hero    string    `json:"hero"`
	EndedAt time.Time `json:"ended_at"`
	Count   int       `json:"count"`
}

type Choice struct {
	Rule    string  `json:"rule"`
	Label   string  `json:"label"`
	Average float64 `json:"average"`
}

// Habits are the rules whose alerts count as a mistake, so they can be drilled.
func Habits(rules []coach.Rule) []coach.Rule {
	var out []coach.Rule
	for _, r := range rules {
		if r.Habit != "" {
			out = append(out, r)
		}
	}
	return out
}

// Countable keeps the matches a drill is scored over: real games the trainer watched, newest
// first, at most Matches of them.
func Countable(recent []model.MatchSummary) []model.MatchSummary {
	out := make([]model.MatchSummary, 0, Matches)
	for _, m := range recent {
		if !m.Real() || !m.Coached() {
			continue
		}
		if out = append(out, m); len(out) == Matches {
			break
		}
	}
	return out
}

// Score builds the view from the drillable rules, the matches from Countable and how often each
// rule fired per match. Passing nothing gives the empty view a failed read shows.
func Score(habits []coach.Rule, recent []model.MatchSummary, fires map[string]map[string]int, chosen string) View {
	v := View{Rule: chosen, Choices: []Choice{}}
	for _, r := range habits {
		per := fires[r.ID]
		total := 0
		for _, m := range recent {
			total += per[m.MatchID]
		}
		avg := 0.0
		if len(recent) > 0 {
			avg = float64(total) / float64(len(recent))
		}
		v.Choices = append(v.Choices, Choice{Rule: r.ID, Label: r.Label, Average: avg})
		if r.ID != chosen {
			continue
		}
		v.Label, v.Advice, v.Average = r.Label, r.Advice, avg
		for i, m := range recent {
			n := per[m.MatchID]
			v.Recent = append(v.Recent, Match{MatchID: m.MatchID, Hero: m.Hero, EndedAt: m.EndedAt, Count: n})
			if i == 0 || n < v.Best {
				v.Best = n
			}
		}
	}
	slices.SortFunc(v.Choices, func(a, b Choice) int { return cmp.Compare(b.Average, a.Average) })
	if len(v.Choices) > 0 && v.Choices[0].Average > 0 {
		v.Suggestion, v.SuggestionLabel = v.Choices[0].Rule, v.Choices[0].Label
	}
	return v
}
