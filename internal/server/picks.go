package server

import (
	"cmp"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"gourdian/internal/coach"
	"gourdian/internal/config"
	"gourdian/internal/dota"
	"gourdian/internal/dotadata"
	"gourdian/internal/gsi"
	"gourdian/internal/picks"
	"gourdian/internal/stats"
)

// pickCache keeps the board, which the draft asks for on every tick, until something it is
// made of changes: the match history, the player's rank, the hero meta, or the day, since old
// games drop out of the window.
type pickCache struct {
	mu    sync.Mutex
	key   string
	board *picks.Board

	// drafting is whether the last update came from a draft, so the picks are read out once
	// when one opens. Dota doesn't always set the match id during hero selection, so the
	// draft is spotted by the change of state rather than by the match it belongs to.
	drafting bool
	// spokenFor is the position the picks were last read out for. Changing position changes
	// the advice entirely, so it is worth hearing again.
	spokenFor string
}

// pickBoard is the heroes worth taking in this position: the player's own record, weighed
// against how each hero is doing at their rank.
func (s *Server) pickBoard(set config.Settings) *picks.Board {
	rank := s.rankTier()
	allies, enemies := s.sides(s.engine.Snapshot(set).MatchID)
	// Everything the board is made of belongs in the key. The rank and the hero meta arrive
	// from OpenDota after the first draft update, so without them an early empty board would
	// be kept all day; without the tuning, changing a setting would appear to do nothing.
	meta := s.data.Meta()
	key := fmt.Sprintf("%s/%d/%d/%d/%+v/%v/%s", set.Role, s.stats.HistoryVersion(), rank, len(meta),
		set.Picks, append(allies, enemies...), time.Now().Format(time.DateOnly))
	c := &s.picks
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.key != key {
		c.key, c.board = key, s.readPickBoard(set, rank, meta, allies, enemies)
	}
	return c.board
}

func (s *Server) readPickBoard(set config.Settings, rank int, meta map[int]dotadata.HeroMeta, allies, enemies []int) *picks.Board {
	history, err := s.stats.MatchesWhere(stats.MatchFilter{Role: set.Role, Since: time.Now().Add(-set.Picks.Window()), Real: true})
	if err != nil {
		s.log.Warn("no match history for pick help", "err", err)
	}
	return picks.Rank(picks.Input{
		Role:     set.Role,
		Lang:     set.Language,
		Rank:     rank,
		Tuning:   set.Picks,
		History:  history,
		Heroes:   s.data.Heroes(),
		Meta:     meta,
		Enemies:  enemies,
		Allies:   allies,
		Matchups: s.matchups(enemies),
	}, time.Now())
}

// matchups is each enemy hero's record against the rest, which is what the pick advice
// weighs a hero against. OpenDota answers in the background, so the first draft against a
// hero says nothing about it and the next one does.
func (s *Server) matchups(enemies []int) map[int]map[int]dotadata.Matchup {
	if len(enemies) == 0 {
		return nil
	}
	out := make(map[int]map[int]dotadata.Matchup, len(enemies))
	for _, id := range enemies {
		if against := s.data.Matchups(id); len(against) > 0 {
			out[id] = against
		}
	}
	return out
}

// rankTier is the player's medal, or 0 while OpenDota hasn't answered.
func (s *Server) rankTier() int {
	acct, _ := s.accountID.Load().(string)
	if acct == "" {
		return 0
	}
	return s.data.RankTier(acct)
}

// draftState reports whether a game state is one Dota shows while the player is still
// choosing a hero.
func draftState(state string) bool {
	switch state {
	case gsi.StateWaitForPlayers, gsi.StateHeroSelection, gsi.StateStrategyTime:
		return true
	}
	return false
}

// pickMatters reports whether the player is still choosing a hero: Dota says it's the draft
// and has no hero for them yet. (It used to ask for a match in progress without a hero, which
// never happens, so pick help never showed.)
func pickMatters(snap coach.Snapshot) bool {
	return snap.Connected && snap.Hero == nil && draftState(snap.GameState)
}

// draftMatters reports whether the draft is still going on, whether or not the player has
// taken a hero. They usually pick early and then watch the rest of it happen, and what the
// other side is taking matters to them the whole time even though their own pick is settled.
func draftMatters(snap coach.Snapshot) bool {
	return snap.Connected && draftState(snap.GameState)
}

// drafting is pickMatters for a game state straight off the wire, so the hot path can tell a
// draft from a match without building a whole snapshot.
func drafting(st *gsi.State) bool {
	if st.Map == nil || (st.Hero != nil && st.Hero.ID != 0) {
		return false
	}
	return draftState(st.Map.GameState)
}

// speakPicks reads the pick advice out, once for each position the player names while they
// are choosing. Naming a position is what makes advice possible at all, and naming a
// different one makes it different advice, so both are worth hearing; saying the same one
// twice is not.
func (s *Server) speakPicks(st *gsi.State, set config.Settings) {
	open := drafting(st)
	c := &s.picks
	c.mu.Lock()
	if !open {
		c.drafting, c.spokenFor = false, ""
		c.mu.Unlock()
		return
	}
	c.drafting = true
	said := c.spokenFor == set.Role
	c.mu.Unlock()
	// Nothing to say until they have named a position, and nothing to add once it has been
	// said for that one. Both checks are cheap, which matters twice a second.
	if said || !s.rolePickedInDraft() {
		return
	}
	snap := s.snapshot(set)
	if snap.Picks == nil || len(snap.Picks.Best) == 0 {
		return
	}
	var names []string
	for _, h := range snap.Picks.Best[:min(len(snap.Picks.Best), 2)] {
		names = append(names, h.Name)
	}
	role := dota.RoleName(set.Role, set.Language)
	text := roleSay(set.Language, "Best %s picks: %s", role, strings.Join(names, " · "))
	speech := roleSay(set.Language, "Best %s picks: %s", role, strings.Join(names, ", ")) + "."
	if len(snap.Picks.Avoid) > 0 {
		avoid := roleSay(set.Language, "Avoid %s", snap.Picks.Avoid[0].Name)
		text, speech = text+" · "+avoid, speech+" "+avoid+"."
	}
	c.mu.Lock()
	c.spokenFor = set.Role
	c.mu.Unlock()
	s.emitTips(snap.MatchID, []coach.Tip{{Rule: "picks", Category: "focus", Severity: coach.Info,
		Clock: snap.Clock, At: time.Now(), Text: text, Speech: speech}}, set)
	s.askDraft(set, false)
}

// handlePicksAsk is the dashboard's "ask the coach" button during a draft.
func (s *Server) handlePicksAsk(w http.ResponseWriter, r *http.Request) {
	set := s.cfg.Settings()
	switch _, _, ok := s.providers.Pick(set.AI.Live, set.AI); {
	case !ok:
		http.Error(w, cmp.Or(s.providers.Banner().Message, "the AI coach isn't connected; set it up on the AI coach page"), http.StatusConflict)
		return
	case !pickMatters(s.snapshot(set)):
		http.Error(w, "the coach can only help while you're choosing a hero", http.StatusConflict)
		return
	}
	if !s.askDraft(set, true) {
		http.Error(w, "the coach is still answering the previous question", http.StatusConflict)
		return
	}
	writeJSON(w, map[string]string{"status": "asking"})
}
