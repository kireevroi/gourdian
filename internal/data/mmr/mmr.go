// Package mmr keeps the prompt asking for your rating after a match, reads what came before,
// and projects how long a rank takes at the pace the log shows.
package mmr

import (
	"math"
	"sync"
	"time"

	"gourdian/internal/game/model"
)

type Prompt struct {
	MatchID string    `json:"match_id"`
	Hero    string    `json:"hero"`
	Result  string    `json:"result"`
	Last    int       `json:"last"`   // the MMR logged before this match
	Ranked  bool      `json:"ranked"` // false until OpenDota confirms it
	At      time.Time `json:"at"`
}

type Prompts struct {
	mu sync.Mutex
	// Never changed in place: a change stores a new one, so a prompt already handed out stays.
	prompt *Prompt
}

func (p *Prompts) Pending() *Prompt {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.prompt
}

func (p *Prompts) Ask(next *Prompt) {
	p.mu.Lock()
	p.prompt = next
	p.mu.Unlock()
}

func (p *Prompts) Clear() {
	p.mu.Lock()
	p.prompt = nil
	p.mu.Unlock()
}

// ConfirmRanked settles the prompt once the kind of match is known, reporting what to announce
// and whether anything changed; a prompt for another match is left alone.
func (p *Prompts) ConfirmRanked(matchID string, ranked bool) (*Prompt, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.prompt == nil || p.prompt.MatchID != matchID {
		return nil, false
	}
	if !ranked {
		p.prompt = nil
		return nil, true
	}
	next := *p.prompt
	next.Ranked = true
	p.prompt = &next
	return &next, true
}

// Before ignores the match's own entry, so logging a match again doesn't add its win twice.
func Before(entries []model.MMREntry, matchID string, ended time.Time) (int, bool) {
	for i := len(entries) - 1; i >= 0; i-- {
		e := entries[i]
		if matchID != "" && e.MatchID == matchID || !ended.IsZero() && e.Date.After(ended) {
			continue
		}
		return e.MMR, true
	}
	return 0, false
}

// For is the MMR logged against a match, or 0.
func For(entries []model.MMREntry, matchID string) int {
	for _, e := range entries {
		if e.MatchID == matchID {
			return e.MMR
		}
	}
	return 0
}

// PaceWindow is how much of the log, back from its latest entry, the pace is measured over.
const PaceWindow = 30 * 24 * time.Hour

const (
	minMatches = 5
	minDays    = 7
)

// What a forecast says about the goal.
const (
	Reached = "reached"
	Early   = "early" // too little logged to measure a pace
	Stalled = "stalled"
	Closing = "closing"
)

type Forecast struct {
	Goal        int       `json:"goal"`
	MMR         int       `json:"mmr"`
	Status      string    `json:"status"`
	Since       time.Time `json:"since"`
	Change      int       `json:"change"`
	Matches     int       `json:"matches"`
	Days        float64   `json:"days"`
	MatchesLeft int       `json:"matches_left,omitempty"`
	DaysLeft    int       `json:"days_left,omitempty"`
}

// Toward measures the pace from an entry, not from the window's edge, so the matches counted
// are the ones whose MMR changes are in Change. It reports false when nothing is logged.
func Toward(goal int, entries []model.MMREntry, matches []model.MatchSummary) (Forecast, bool) {
	if len(entries) == 0 {
		return Forecast{}, false
	}
	last, from := entries[len(entries)-1], entries[0]
	for _, e := range entries {
		if e.Date.After(last.Date.Add(-PaceWindow)) {
			break
		}
		from = e
	}
	f := Forecast{Goal: goal, MMR: last.MMR, Since: from.Date, Change: last.MMR - from.MMR,
		Days: last.Date.Sub(from.Date).Hours() / 24}
	logged := map[string]bool{}
	for _, e := range entries {
		logged[e.MatchID] = e.MatchID != ""
	}
	for _, m := range matches {
		if m.Real() && (m.Ranked || logged[m.MatchID]) && m.EndedAt.After(from.Date) && !m.EndedAt.After(last.Date) {
			f.Matches++
		}
	}
	gap := goal - last.MMR
	switch {
	case gap <= 0:
		f.Status = Reached
	case f.Matches < minMatches && f.Days < minDays:
		f.Status = Early
	case f.Change <= 0:
		f.Status = Stalled
	default:
		f.Status = Closing
		if f.Matches >= minMatches {
			f.MatchesLeft = (gap*f.Matches + f.Change - 1) / f.Change
		}
		if f.Days >= minDays {
			f.DaysLeft = int(math.Ceil(float64(gap) * f.Days / float64(f.Change)))
		}
	}
	return f, true
}
