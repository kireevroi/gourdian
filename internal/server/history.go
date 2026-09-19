package server

import (
	"cmp"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"time"

	"gourdian/internal/coach"
	"gourdian/internal/model"
	"gourdian/internal/stats"
)

type Habit struct {
	Rule     string  `json:"rule"`
	Label    string  `json:"label"`
	Advice   string  `json:"advice"`
	PerMatch float64 `json:"per_match"`
	Matches  int     `json:"matches"`
}

type summary struct {
	Habits        []Habit `json:"habits"`
	Sample        int     `json:"sample"`
	FromSimulated bool    `json:"from_simulated"`
	WinRate       float64 `json:"win_rate"`
	AvgDeaths     float64 `json:"avg_deaths"`
	AvgGPM        float64 `json:"avg_gpm"`
	AvgLH10       float64 `json:"avg_lh10"`
}

const habitSample = 10

// summarize describes the last few real matches, falling back to simulated ones so the
// dashboard has something to show before the first real game.
func summarize(recent []model.MatchSummary, rules []coach.Rule) summary {
	var sum summary
	var sample []model.MatchSummary
	for _, m := range recent {
		if m.Real() && len(sample) < habitSample {
			sample = append(sample, m)
		}
	}
	if len(sample) == 0 {
		sample, sum.FromSimulated = recent[:min(len(recent), habitSample)], len(recent) > 0
	}
	sum.Sample = len(sample)
	sum.Habits = habits(slices.DeleteFunc(slices.Clone(sample), func(m model.MatchSummary) bool { return !m.Coached() }), rules)
	var wins, decided, lh10n int
	for _, m := range sample {
		sum.AvgDeaths += float64(m.Deaths)
		sum.AvgGPM += float64(m.GPM)
		if lh, ok := m.LastHitsAt["10:00"]; ok {
			sum.AvgLH10 += float64(lh)
			lh10n++
		}
		if m.Result != "unknown" {
			decided++
			if m.Result == "win" {
				wins++
			}
		}
	}
	if n := float64(len(sample)); n > 0 {
		sum.AvgDeaths /= n
		sum.AvgGPM /= n
	}
	if lh10n > 0 {
		sum.AvgLH10 /= float64(lh10n)
	}
	if decided > 0 {
		sum.WinRate = float64(wins) / float64(decided)
	}
	return sum
}

func habits(sample []model.MatchSummary, rules []coach.Rule) []Habit {
	var out []Habit
	for _, r := range rules {
		if r.Advice == "" {
			continue
		}
		h := Habit{Rule: r.ID, Label: cmp.Or(r.Habit, r.Label), Advice: r.Advice}
		total := 0
		for _, m := range sample {
			if n := m.TipCounts[r.ID]; n > 0 {
				total += n
				h.Matches++
			}
		}
		if total == 0 {
			continue
		}
		h.PerMatch = float64(total) / float64(len(sample))
		out = append(out, h)
	}
	slices.SortFunc(out, func(a, b Habit) int { return cmp.Compare(b.PerMatch, a.PerMatch) })
	return out[:min(len(out), 5)]
}

func (s *Server) handleHistory(w http.ResponseWriter, r *http.Request) {
	recent, err := s.stats.Recent(50)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, struct {
		Matches []model.MatchSummary `json:"matches"`
		summary
	}{recent[:min(len(recent), 20)], summarize(recent, s.engine.Rules())})
}

type statsFile struct {
	Name     string    `json:"name"`
	Size     int64     `json:"size"`
	Modified time.Time `json:"modified"`
}

type ruleLabel struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

type statsResponse struct {
	Dir     string               `json:"dir"`
	Files   []statsFile          `json:"files"`
	Matches []model.MatchSummary `json:"matches"`
	MMR     []model.MMREntry     `json:"mmr"`
	Habits  []ruleLabel          `json:"habits"`
	// LastHitsByMinute maps match id to last hits at each minute (-1 where no sample was taken).
	LastHitsByMinute map[string][]int `json:"last_hits_by_minute"`
}

func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	resp := statsResponse{Dir: s.stats.Dir(), LastHitsByMinute: map[string][]int{}}
	var err error
	if resp.Matches, err = s.stats.Matches(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if resp.MMR, err = s.stats.MMR(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	timeline, err := s.stats.Timeline()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	for _, x := range timeline {
		minute := x.Clock / 60
		if minute > 90 {
			continue
		}
		curve := resp.LastHitsByMinute[x.MatchID]
		for len(curve) <= minute {
			curve = append(curve, -1)
		}
		curve[minute] = x.LastHits
		resp.LastHitsByMinute[x.MatchID] = curve
	}
	for _, name := range stats.Files {
		if fi, err := os.Stat(filepath.Join(s.stats.Dir(), name)); err == nil {
			resp.Files = append(resp.Files, statsFile{Name: name, Size: fi.Size(), Modified: fi.ModTime()})
		}
	}
	for _, rule := range s.engine.Rules() {
		if rule.Advice != "" {
			resp.Habits = append(resp.Habits, ruleLabel{rule.ID, cmp.Or(rule.Habit, rule.Label)})
		}
	}
	writeJSON(w, resp)
}

func (s *Server) handleStatsFile(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if !slices.Contains(stats.Files, name) {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	http.ServeFile(w, r, filepath.Join(s.stats.Dir(), name))
}
