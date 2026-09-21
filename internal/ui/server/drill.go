package server

import (
	"net/http"
	"slices"
	"time"

	"gourdian/internal/coaching/coach"
	"gourdian/internal/coaching/drill"
	"gourdian/internal/game/model"
	"gourdian/internal/sys/config"
)

func (s *Server) habitRules() []coach.Rule { return drill.Habits(s.engine.Rules()) }

func (s *Server) drill() drill.View {
	set := s.cfg.Settings()
	recent, err := s.stats.Recent(drill.Window)
	if err != nil {
		return drill.Score(nil, nil, nil, set.Drill)
	}
	recent = drill.Countable(recent)
	ids := make([]string, len(recent))
	for i, m := range recent {
		ids[i] = m.MatchID
	}
	fires, err := s.stats.FiresIn(ids)
	if err != nil {
		return drill.Score(nil, nil, nil, set.Drill)
	}
	return drill.Score(s.habitRules(), recent, fires, set.Drill)
}

func (s *Server) handleDrill(w http.ResponseWriter, r *http.Request) { writeJSON(w, s.drill()) }

func (s *Server) handleSetDrill(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Rule string `json:"rule"`
	}
	if err := readJSON(w, r, 4<<10, &body); err != nil {
		http.Error(w, `send {"rule": "no_tp"} or {"rule": ""}`, http.StatusBadRequest)
		return
	}
	if body.Rule != "" && !slices.ContainsFunc(s.habitRules(), func(x coach.Rule) bool { return x.ID == body.Rule }) {
		http.Error(w, "that rule doesn't count mistakes, so it can't be drilled", http.StatusBadRequest)
		return
	}
	if _, err := s.cfg.Update(func(set *config.Settings) error {
		set.Drill = body.Rule
		return nil
	}); err != nil {
		s.settingsProblem(w, err)
		return
	}
	s.publishSettings()
	s.dirty.Store(true)
	writeJSON(w, s.drill())
}

// drillResult is the line the player hears after a match they drilled.
func (s *Server) drillResult(m model.MatchSummary, set config.Settings) {
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
	line := func(lang string) string {
		switch {
		case before > 0 && float64(count) < before:
			return roleSay(lang, "Drill: %s %d times, under your usual %.1f", v.Label, count, before)
		case before > 0 && float64(count) > before:
			return roleSay(lang, "Drill: %s %d times, above your usual %.1f", v.Label, count, before)
		}
		return roleSay(lang, "Drill: %s %d times this game", v.Label, count)
	}
	tip := coach.Tip{Rule: "drill", Category: "focus", Severity: coach.Info, Clock: m.DurationSec, At: time.Now(),
		Text: line(set.Language), Speech: line(set.Language)}
	if set.Language != "en" {
		tip.SpeechEN = line("en")
	}
	s.emitTips(m.MatchID, []coach.Tip{tip}, set)
}
