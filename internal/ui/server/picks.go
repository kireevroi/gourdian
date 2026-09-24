package server

import (
	"cmp"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"gourdian/internal/coaching/coach"
	"gourdian/internal/coaching/picks"
	"gourdian/internal/data/opendota"
	"gourdian/internal/data/stats"
	"gourdian/internal/game/dota"
	"gourdian/internal/game/gsi"
	"gourdian/internal/i18n"
	"gourdian/internal/sys/config"
)

// pickCache keeps the board until something it is made of changes.
type pickCache struct {
	mu    sync.Mutex
	key   string
	board *picks.Board

	// Dota doesn't always set the match id during hero selection, so the draft is spotted by
	// the change of state.
	drafting      bool
	spokenFor     string
	spokenAgainst []int
	seen          []int
	seenAt        time.Time
}

// waveQuiet gathers a wave of enemy picks into one reading.
const waveQuiet = 6 * time.Second

func (c *pickCache) sayNow(role string, enemies []int, now time.Time) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !slices.Equal(enemies, c.seen) {
		c.seen, c.seenAt = slices.Clone(enemies), now
	}
	switch {
	case c.spokenFor != role:
		// Only growth: a stale reading shrinks the side, and its recovery is not a new wave.
	case len(enemies) > len(c.spokenAgainst) && now.Sub(c.seenAt) >= waveQuiet:
	default:
		return false
	}
	c.spokenFor, c.spokenAgainst = role, slices.Clone(enemies)
	return true
}

func (c *pickCache) forget() {
	c.mu.Lock()
	c.drafting, c.spokenFor, c.spokenAgainst, c.seen = false, "", nil, nil
	c.mu.Unlock()
}

func (s *Server) pickBoard(set config.Settings) *picks.Board {
	rank := s.rankTier()
	allies, enemies := s.sides(s.engine.Snapshot(set).MatchID)
	// Rank and meta arrive after the first draft update; leave them out and an empty board
	// sticks all day.
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

func (s *Server) readPickBoard(set config.Settings, rank int, meta map[int]opendota.HeroMeta, allies, enemies []int) *picks.Board {
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

func (s *Server) matchups(enemies []int) map[int]map[int]opendota.Matchup {
	if len(enemies) == 0 {
		return nil
	}
	out := make(map[int]map[int]opendota.Matchup, len(enemies))
	for _, id := range enemies {
		if against := s.data.Matchups(id); len(against) > 0 {
			out[id] = against
		}
	}
	return out
}

func (s *Server) rankTier() int {
	acct, _ := s.accountID.Load().(string)
	if acct == "" {
		return 0
	}
	return s.data.RankTier(acct)
}

func draftState(state string) bool {
	switch state {
	case gsi.StateWaitForPlayers, gsi.StateHeroSelection, gsi.StateStrategyTime:
		return true
	}
	return false
}

func pickMatters(snap coach.Snapshot) bool {
	return snap.Connected && snap.Hero == nil && draftState(snap.GameState)
}

func draftMatters(snap coach.Snapshot) bool {
	return snap.Connected && draftState(snap.GameState)
}

func drafting(st *gsi.State) bool {
	if st.Map == nil || (st.Hero != nil && st.Hero.ID != 0) {
		return false
	}
	return draftState(st.Map.GameState)
}

func (s *Server) speakPicks(st *gsi.State, set config.Settings) {
	c := &s.picks
	if !drafting(st) {
		c.forget()
		return
	}
	c.mu.Lock()
	c.drafting = true
	c.mu.Unlock()
	if !s.role.Mine(set.Role) {
		return
	}
	snap := s.snapshot(set)
	if snap.Picks == nil || len(snap.Picks.Best) == 0 {
		return
	}
	against := make([]int, 0, len(snap.Picks.Enemies))
	for _, h := range snap.Picks.Enemies {
		against = append(against, h.ID)
	}
	if !c.sayNow(set.Role, against, time.Now()) {
		return
	}
	text, speech := pickLine(snap.Picks, set.Role, set.Language)
	s.emitTips(snap.MatchID, []coach.Tip{{Rule: "picks", Category: "focus", Severity: coach.Info,
		Clock: snap.Clock, At: time.Now(), Text: text, Speech: speech}}, set)
	s.askDraft(set, false)
}

// sayHeroes is four because bans can't be seen: two suggestions were often both banned.
const sayHeroes = 4

// pickLine puts the other side first, since their picks are what prompts a second reading.
func pickLine(b *picks.Board, role, lang string) (text, speech string) {
	var names, strangers []string
	for _, h := range b.Best[:min(len(b.Best), sayHeroes)] {
		names = append(names, h.Name)
	}
	for _, h := range b.Fresh[:min(len(b.Fresh), sayHeroes-len(names))] {
		strangers = append(strangers, h.Name)
	}
	named := dota.RoleName(role, lang)
	text = i18n.Say(lang, "Best %s picks: %s", named, strings.Join(names, " · "))
	speech = i18n.Say(lang, "Best %s picks: %s", named, strings.Join(names, ", ")) + "."
	if len(strangers) > 0 {
		fresh := i18n.Say(lang, "New to you: %s", strings.Join(strangers, ", "))
		text, speech = text+" · "+fresh, speech+" "+fresh+"."
	}
	if len(b.Avoid) > 0 {
		avoid := i18n.Say(lang, "Avoid %s", b.Avoid[0].Name)
		text, speech = text+" · "+avoid, speech+" "+avoid+"."
	}
	var them []string
	for _, h := range b.Enemies {
		// A hero the trainer has no name for would otherwise read out as a pause.
		if h.Name != "" {
			them = append(them, h.Name)
		}
	}
	if len(them) > 0 {
		had := i18n.Say(lang, "Against: %s", strings.Join(them, ", "))
		text, speech = had+" · "+text, had+". "+speech
	}
	return text, speech
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
