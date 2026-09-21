// Package mmr keeps the prompt asking for your rating after a match, and reads what came before.
package mmr

import (
	"sync"
	"time"

	"gourdian/internal/model"
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
