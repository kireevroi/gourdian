package server

import (
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"gourdian/internal/coach"
	"gourdian/internal/config"
	"gourdian/internal/dota"
	"gourdian/internal/model"
	"gourdian/internal/stats"
)

const briefingFresh = 10 * time.Second

// briefingCache holds the latest briefing and the per-session state around it.
type briefingCache struct {
	mu     sync.Mutex
	key    string
	at     time.Time
	b      *coach.Briefing
	spoken string    // match whose briefing was spoken
	tiltAt time.Time // when the last break warning was given
}

// snapshot is the engine's snapshot plus what only the trainer knows, like the briefing.
func (s *Server) snapshot(set config.Settings) coach.Snapshot {
	snap := s.engine.Snapshot(set)
	switch {
	case pickMatters(snap):
		snap.Picks = s.pickBoard(set)
	case draftMatters(snap):
		// Their own pick is made, so the advice about what to take goes; who they are up
		// against does not.
		snap.Picks = s.pickBoard(set).AfterYourPick()
	}
	if snap.InMatch && snap.Hero != nil && snap.Clock < 0 {
		snap.Briefing = s.briefing(snap.Hero.ID, snap.Hero.Name, set.Role)
	}
	return snap
}

func (s *Server) briefing(heroID int, hero, role string) *coach.Briefing {
	key := fmt.Sprintf("%d/%s", heroID, role)
	c := &s.brief
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.key == key && time.Since(c.at) < briefingFresh {
		return c.b
	}
	b := &coach.Briefing{Hero: hero, Role: role}
	if matches, err := s.stats.MatchesWhere(stats.MatchFilter{HeroID: heroID, Role: role, Real: true}); err == nil {
		for _, m := range matches {
			b.Games++
			if m.Result == "win" {
				b.Wins++
			}
		}
	}
	t := s.targets.TargetsFor(heroID, role)
	if len(t.LastHits) > 1 {
		b.Target10 = t.LastHits[1]
		if len(t.Usual) > 1 {
			b.Usual10 = t.Usual[1]
		}
	}
	b.Items = t.Items
	for _, g := range s.weekProgress(time.Now()) {
		if !g.Done {
			b.Goals = append(b.Goals, g)
		}
	}
	if reviews, err := s.stats.Reviews(); err == nil {
		for _, r := range slices.Backward(reviews) {
			if r.Hero == hero {
				b.LastReview = r.NextGameFocus
				break
			}
		}
	}
	c.key, c.at, c.b = key, time.Now(), b
	return b
}

// briefMatch speaks the briefing once, at the first update before the horn.
func (s *Server) briefMatch(matchID string, set config.Settings) {
	s.brief.mu.Lock()
	done := s.brief.spoken == matchID
	s.brief.spoken = matchID
	s.brief.mu.Unlock()
	if done {
		return
	}
	snap := s.snapshot(set)
	b := snap.Briefing
	if b == nil {
		return
	}
	text, speech := briefingWords(b, set.Language)
	if text == "" {
		return
	}
	tip := coach.Tip{Rule: "briefing", Category: "focus", Severity: coach.Info, Clock: snap.Clock, At: time.Now(),
		Text: text, Speech: speech}
	if set.Language != "en" {
		_, tip.SpeechEN = briefingWords(b, "en")
	}
	s.emitTips(matchID, []coach.Tip{tip}, set)
}

// briefingWords is the briefing as the player reads and hears it, in their language. It is
// empty when there is nothing worth saying before the horn.
func briefingWords(b *coach.Briefing, lang string) (text, speech string) {
	var parts, said []string
	if b.Target10 > 0 {
		parts = append(parts, roleSay(lang, "aim for %d last hits at 10:00", b.Target10))
		said = append(said, roleSay(lang, "Aim for %d last hits at ten minutes.", b.Target10))
	}
	for _, it := range b.Items[:min(len(b.Items), 1)] {
		parts = append(parts, roleSay(lang, "%s by %s", it.Name, dota.Clock(it.By)))
		said = append(said, roleSay(lang, "%s by %d minutes.", it.Name, (it.By+30)/60))
	}
	for _, g := range b.Goals[:min(len(b.Goals), 1)] {
		parts = append(parts, roleSay(lang, "goal: %s (%d/%d)", g.Label, g.Met, model.GoalsDone))
		said = append(said, roleSay(lang, "This week's goal: %s.", g.Label))
	}
	if len(parts) == 0 {
		return "", ""
	}
	return b.Hero + ": " + strings.Join(parts, " · "), strings.Join(said, " ")
}
