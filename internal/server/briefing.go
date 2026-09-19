package server

import (
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"dotatrainer/internal/coach"
	"dotatrainer/internal/config"
	"dotatrainer/internal/stats"
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
	if pickMatters(snap) {
		snap.Picks = s.pickHelp(set.Role)
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
	if matches, err := s.stats.Matches(); err == nil {
		for _, m := range matches {
			if m.HeroID == heroID && m.Role == role && m.Real() {
				b.Games++
				if m.Result == "win" {
					b.Wins++
				}
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
	var text, speech []string
	if b.Target10 > 0 {
		text = append(text, fmt.Sprintf("aim for %d last hits at 10:00", b.Target10))
		speech = append(speech, fmt.Sprintf("Aim for %d last hits at ten minutes.", b.Target10))
	}
	for _, it := range b.Items[:min(len(b.Items), 1)] {
		text = append(text, fmt.Sprintf("%s by %s", it.Name, clockText(it.By)))
		speech = append(speech, fmt.Sprintf("%s by %d minutes.", it.Name, (it.By+30)/60))
	}
	for _, g := range b.Goals[:min(len(b.Goals), 1)] {
		text = append(text, fmt.Sprintf("goal: %s (%d/%d)", g.Label, g.Met, stats.GoalsDone))
		speech = append(speech, "This week's goal: "+g.Label+".")
	}
	if len(text) == 0 {
		return
	}
	tip := coach.Tip{Rule: "briefing", Category: "focus", Severity: coach.Info, Clock: snap.Clock, At: time.Now(),
		Text: b.Hero + ": " + strings.Join(text, " · "), Speech: strings.Join(speech, " ")}
	s.engine.AddTips([]coach.Tip{tip})
	s.deliver(matchID, []coach.Tip{tip}, set)
}

func clockText(sec int) string { return fmt.Sprintf("%d:%02d", sec/60, sec%60) }
