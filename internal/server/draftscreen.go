package server

import (
	"encoding/json"
	"net/http"
	"slices"
	"sync"
	"time"

	"gourdian/internal/config"
	"gourdian/internal/screen"
)

// draftBoard holds what the overlay has read off the screen during the current draft. It is
// the only way a player's tools can learn what the other side took: Valve sends the draft to
// spectators, not to players, so the trainer reads the portraits the game has already drawn.
type draftBoard struct {
	mu      sync.Mutex
	matchID string
	at      time.Time
	ours    []int
	theirs  []int
}

// draftFresh is how long a reading stands without being renewed. The overlay sends one every
// couple of seconds while the draft is open; once it stops, the board goes with it.
const draftFresh = 20 * time.Second

// enemies is who the other side has taken, or nothing when the screen hasn't been read or the
// reading has gone stale.
func (s *Server) enemies(matchID string) []int {
	b := &s.draft
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.matchID != matchID || time.Since(b.at) > draftFresh {
		return nil
	}
	return slices.Clone(b.theirs)
}

// handleHeroNames gives the overlay the hero numbers, which it needs to say what it saw and
// which only this side of the trainer fetches.
func (s *Server) handleHeroNames(w http.ResponseWriter, r *http.Request) {
	ids := map[string]int{}
	for _, h := range s.data.Heroes() {
		ids[h.Name] = h.ID
	}
	writeJSON(w, ids)
}

// handleDraftSeen takes a reading from the overlay, which is the part of the trainer running
// where the screen is.
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
		b.ours, b.theirs = nil, nil
	}
	b.matchID, b.at = snap.MatchID, time.Now()
	b.ours, b.theirs = keepHeroes(seen.Ours), keepHeroes(seen.Theirs)
	found := len(b.theirs)
	b.mu.Unlock()

	// Where the portraits turned out to be is worth keeping: the next draft on this screen
	// then costs nothing to find.
	if seen.Bar != nil && seen.Screen != "" && seen.Bar.Ready() {
		s.rememberBar(seen.Screen, *seen.Bar)
	}
	if found > 0 {
		s.dirty.Store(true)
	}
	writeJSON(w, map[string]int{"theirs": found})
}

// keepHeroes drops repeats and anything that isn't a hero id, so a bad reading can't put a
// number the rest of the trainer has never heard of into the pick advice.
func keepHeroes(ids []int) []int {
	var out []int
	for _, id := range ids {
		if id > 0 && !slices.Contains(out, id) {
			out = append(out, id)
		}
	}
	return out[:min(len(out), screen.Slots)]
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
