package server

import (
	"fmt"
	"maps"
	"net/http"
	"slices"
	"time"

	"gourdian/internal/aicoach"
	"gourdian/internal/coach"
	"gourdian/internal/config"
	"gourdian/internal/model"
	"gourdian/internal/stats"
)

// goalMetrics lists what review goals may measure: fixed stats plus each habit's warnings.
func (s *Server) goalMetrics() map[string]string {
	metrics := maps.Clone(model.GoalMetrics)
	for _, r := range s.engine.Rules() {
		if r.Habit != "" {
			metrics["mistakes_"+r.ID] = "times warned about: " + r.Habit
		}
	}
	return metrics
}

func (s *Server) weekProgress(now time.Time) []model.GoalProgress {
	all, err := s.stats.Goals()
	if err != nil {
		return nil
	}
	// Progress picks the week's games itself; the last eight days hold them all.
	matches, _ := s.stats.MatchesWhere(stats.MatchFilter{Since: now.AddDate(0, 0, -8)})
	return model.Progress(model.WeekGoals(all, model.Week(now)), matches)
}

// saveGoals stores the goals a review set for the rest of the week.
func (s *Server) saveGoals(r aicoach.Review, matchID string, now time.Time) {
	goals := make([]model.Goal, 0, len(r.Goals))
	for _, g := range r.Goals {
		goals = append(goals, model.Goal{Created: now, Week: model.Week(now), Metric: g.Metric, Comparator: g.Comparator,
			Target: g.Target, Label: g.Label, MatchID: matchID})
	}
	if err := s.stats.AppendGoals(goals); err != nil {
		s.log.Error("save goals", "err", err)
		return
	}
	s.hub.publish("goals", s.weekProgress(now))
}

// goalFeedback tells the player, right after a match, how it did against the week's goals.
func (s *Server) goalFeedback(m model.MatchSummary, set config.Settings) {
	if !m.Real() {
		return
	}
	var tips []coach.Tip
	for _, p := range s.weekProgress(m.EndedAt) {
		met, ok := p.Goal.Met(m)
		if !ok || m.MatchID == p.MatchID {
			continue
		}
		v, _ := model.MetricValue(m, p.Metric)
		tip := coach.Tip{Rule: "goals", Category: "focus", Severity: coach.Info, Clock: m.DurationSec, At: time.Now()}
		switch {
		case met && p.Met == model.GoalsDone:
			tip.Text = fmt.Sprintf("Weekly goal done: %s, met in %d matches", p.Label, model.GoalsDone)
			tip.Speech = "Weekly goal done. " + p.Label
		case met:
			tip.Text = fmt.Sprintf("Goal met: %s (%d of %d this week)", p.Label, min(p.Met, model.GoalsDone), model.GoalsDone)
			tip.Speech = fmt.Sprintf("Goal met. %d of %d this week.", min(p.Met, model.GoalsDone), model.GoalsDone)
		default:
			tip.Text = fmt.Sprintf("Goal missed: %s (you had %g)", p.Label, v)
			tip.Quiet = true
		}
		tips = append(tips, tip)
	}
	if len(tips) == 0 {
		return
	}
	s.emitTips(m.MatchID, tips, set)
	s.hub.publish("goals", s.weekProgress(time.Now()))
}

type goalsResponse struct {
	Week     string               `json:"week"`
	Done     int                  `json:"done_after"`
	Progress []model.GoalProgress `json:"progress"`
	Metrics  map[string]string    `json:"metrics"`
	History  []model.Goal         `json:"history"`
}

func (s *Server) handleGoals(w http.ResponseWriter, r *http.Request) {
	now := time.Now()
	all, _ := s.stats.Goals()
	slices.Reverse(all)
	writeJSON(w, goalsResponse{Week: model.Week(now), Done: model.GoalsDone, Progress: s.weekProgress(now),
		Metrics: s.goalMetrics(), History: all[:min(len(all), 30)]})
}
