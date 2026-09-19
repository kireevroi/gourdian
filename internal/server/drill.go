package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"time"

	"dotatrainer/internal/coach"
	"dotatrainer/internal/config"
	"dotatrainer/internal/stats"
)

// drillMatches is how many recent matches the drill is scored over.
const drillMatches = 10

// drillView is the habit the player is working on: what it is, how it went in recent
// matches, and which habit the trainer would pick if they haven't chosen one.
type drillView struct {
	Rule            string        `json:"rule,omitempty"`
	Label           string        `json:"label,omitempty"`
	Advice          string        `json:"advice,omitempty"`
	Average         float64       `json:"average"`
	Best            int           `json:"best"`
	Recent          []drillMatch  `json:"recent,omitempty"`
	Suggestion      string        `json:"suggestion,omitempty"`
	SuggestionLabel string        `json:"suggestion_label,omitempty"`
	Choices         []drillChoice `json:"choices"`
}

type drillMatch struct {
	MatchID string    `json:"match_id"`
	Hero    string    `json:"hero"`
	EndedAt time.Time `json:"ended_at"`
	Count   int       `json:"count"`
}

type drillChoice struct {
	Rule    string  `json:"rule"`
	Label   string  `json:"label"`
	Average float64 `json:"average"`
}

// habitRules are the rules whose alerts count as a mistake, newest wording first.
func (s *Server) habitRules() []coach.Rule {
	var out []coach.Rule
	for _, r := range s.engine.Rules() {
		if r.Habit != "" {
			out = append(out, r)
		}
	}
	return out
}

func (s *Server) drill() drillView {
	set := s.cfg.Settings()
	v := drillView{Rule: set.Drill, Choices: []drillChoice{}}
	recent, err := s.stats.Recent(drillMatches * 3)
	if err != nil {
		return v
	}
	recent = slices.DeleteFunc(recent, func(m stats.MatchSummary) bool { return !m.Real() || !m.Coached() })
	if len(recent) > drillMatches {
		recent = recent[:drillMatches]
	}
	ids := make([]string, len(recent))
	for i, m := range recent {
		ids[i] = m.MatchID
	}
	byRule, err := s.stats.FiresIn(ids)
	if err != nil {
		return v
	}
	for _, r := range s.habitRules() {
		fires := byRule[r.ID]
		total := 0
		for _, m := range recent {
			total += fires[m.MatchID]
		}
		avg := 0.0
		if len(recent) > 0 {
			avg = float64(total) / float64(len(recent))
		}
		v.Choices = append(v.Choices, drillChoice{Rule: r.ID, Label: r.Label, Average: avg})
		if r.ID != set.Drill {
			continue
		}
		v.Label, v.Advice, v.Average, v.Best = r.Label, r.Advice, avg, -1
		for _, m := range recent {
			n := fires[m.MatchID]
			v.Recent = append(v.Recent, drillMatch{MatchID: m.MatchID, Hero: m.Hero, EndedAt: m.EndedAt, Count: n})
			if v.Best < 0 || n < v.Best {
				v.Best = n
			}
		}
		if v.Best < 0 {
			v.Best = 0
		}
	}
	slices.SortFunc(v.Choices, func(a, b drillChoice) int {
		switch {
		case a.Average > b.Average:
			return -1
		case a.Average < b.Average:
			return 1
		}
		return 0
	})
	if len(v.Choices) > 0 && v.Choices[0].Average > 0 {
		v.Suggestion, v.SuggestionLabel = v.Choices[0].Rule, v.Choices[0].Label
	}
	return v
}

func (s *Server) handleDrill(w http.ResponseWriter, r *http.Request) { writeJSON(w, s.drill()) }

func (s *Server) handleSetDrill(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Rule string `json:"rule"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&body); err != nil {
		http.Error(w, `send {"rule": "no_tp"} or {"rule": ""}`, http.StatusBadRequest)
		return
	}
	if body.Rule != "" && !slices.ContainsFunc(s.habitRules(), func(x coach.Rule) bool { return x.ID == body.Rule }) {
		http.Error(w, "that rule doesn't count mistakes, so it can't be drilled", http.StatusBadRequest)
		return
	}
	set := s.cfg.Settings()
	set.Drill = body.Rule
	if err := s.cfg.UpdateSettings(set); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.hub.publish("settings", s.settingsResponse())
	s.dirty.Store(true)
	writeJSON(w, s.drill())
}

// drillResult is the line the player hears after a match they drilled.
func (s *Server) drillResult(m stats.MatchSummary, set config.Settings) {
	if set.Drill == "" || !m.Real() {
		return
	}
	v := s.drill()
	count := m.TipCounts[set.Drill]
	if fires, err := s.stats.FiresIn([]string{m.MatchID}); err == nil {
		if n, ok := fires[set.Drill][m.MatchID]; ok {
			count = n
		}
	}
	before := v.Average
	text := fmt.Sprintf("Drill: %s %d times this game", v.Label, count)
	switch {
	case before <= 0:
	case float64(count) < before:
		text = fmt.Sprintf("Drill: %s %d times, under your usual %.1f", v.Label, count, before)
	case float64(count) > before:
		text = fmt.Sprintf("Drill: %s %d times, above your usual %.1f", v.Label, count, before)
	}
	s.engine.AddTips([]coach.Tip{{Rule: "drill", Category: "focus", Severity: coach.Info, Clock: m.DurationSec,
		At: time.Now(), Text: text, Speech: text, Quiet: false}})
}
