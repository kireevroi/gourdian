package server

import (
	"encoding/json"
	"net/http"
	"slices"
	"strconv"
	"sync"
	"time"

	"gourdian/internal/sys/config"
	"gourdian/internal/ui/screen"
)

// draftBoard is the draft read off the screen, since Valve sends it to spectators, not players.
type draftBoard struct {
	mu      sync.Mutex
	matchID string
	at      time.Time
	ours    sightings
	theirs  sightings
}

// sightings is one side of the draft: a hero joins once two readings in a row show it, then stays,
// since a pick can't be undone and a flickering portrait mustn't take it off the board.
type sightings struct {
	kept []int
	last []int // heroes the previous reading showed that aren't kept yet
}

func (s *sightings) see(reading []int) {
	var fresh []int
	for _, id := range reading {
		switch {
		case slices.Contains(s.kept, id):
		case slices.Contains(s.last, id) && len(s.kept) < screen.Slots:
			s.kept = append(s.kept, id)
		default:
			fresh = append(fresh, id)
		}
	}
	s.last = fresh
}

// draftFresh is how long a reading stands unrenewed; the overlay sends one every couple of seconds.
const draftFresh = 20 * time.Second

// sides is who each team has taken, or nothing when the reading is missing or stale.
func (s *Server) sides(matchID string) (ours, theirs []int) {
	b := &s.draft
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.matchID != matchID || time.Since(b.at) > draftFresh {
		return nil, nil
	}
	return slices.Clone(b.ours.kept), slices.Clone(b.theirs.kept)
}

// handleHeroNames gives the overlay the hero ids it names readings by, which only this side fetches.
func (s *Server) handleHeroNames(w http.ResponseWriter, r *http.Request) {
	ids := map[string]int{}
	for _, h := range s.data.Heroes() {
		ids[h.Name] = h.ID
	}
	writeJSON(w, ids)
}

// handleDraftSeen takes a reading from the overlay, the part of the trainer where the screen is.
func (s *Server) handleDraftSeen(w http.ResponseWriter, r *http.Request) {
	var seen struct {
		Ours   []int       `json:"ours"`
		Theirs []int       `json:"theirs"`
		Screen string      `json:"screen"`
		Bar    *screen.Bar `json:"bar,omitempty"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(&seen); err != nil {
		http.Error(w, "bad reading: "+err.Error(), http.StatusBadRequest)
		return
	}
	set := s.cfg.Settings()
	if !set.Screen.Draft {
		http.Error(w, "reading the screen is turned off", http.StatusConflict)
		return
	}
	snap := s.engine.Snapshot(set)
	b := &s.draft
	b.mu.Lock()
	if b.matchID != snap.MatchID {
		b.ours, b.theirs = sightings{}, sightings{}
	}
	b.matchID, b.at = snap.MatchID, time.Now()
	was := slices.Concat(b.ours.kept, b.theirs.kept)
	b.ours.see(keepHeroes(seen.Ours))
	b.theirs.see(keepHeroes(seen.Theirs))
	ours, theirs := slices.Clone(b.ours.kept), slices.Clone(b.theirs.kept)
	found := len(theirs)
	b.mu.Unlock()

	// A hero read off the screen can't be checked against Dota, so log it: a wrong one shows later.
	if !slices.Equal(was, slices.Concat(ours, theirs)) {
		s.log.Info("read the draft off the screen", "ours", s.named(ours), "theirs", s.named(theirs))
	}

	// Remember where the portraits were, so the next draft on this screen costs nothing to find.
	if seen.Bar != nil && seen.Screen != "" && seen.Bar.Ready() {
		s.rememberBar(seen.Screen, *seen.Bar)
	}
	if found > 0 {
		s.dirty.Store(true)
	}
	writeJSON(w, map[string]int{"theirs": found})
}

// keepHeroes drops repeats and non-hero ids, so a bad reading can't reach the pick advice.
func keepHeroes(ids []int) []int {
	var out []int
	for _, id := range ids {
		if id > 0 && !slices.Contains(out, id) {
			out = append(out, id)
		}
	}
	return out[:min(len(out), screen.Slots)]
}

func (s *Server) named(ids []int) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if s.data == nil {
			out = append(out, strconv.Itoa(id))
			continue
		}
		out = append(out, s.data.HeroName(id))
	}
	return out
}

func (s *Server) rememberBar(size string, bar screen.Bar) {
	set := s.cfg.Settings()
	if old, ok := set.Screen.Bars[size]; ok && old == bar {
		return
	}
	if _, err := s.cfg.Update(func(set *config.Settings) error {
		if set.Screen.Bars == nil {
			set.Screen.Bars = map[string]screen.Bar{}
		}
		set.Screen.Bars[size] = bar
		return nil
	}); err != nil {
		s.log.Warn("couldn't remember where the hero portraits are", "screen", size, "err", err)
	}
}
