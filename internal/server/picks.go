package server

import (
	"fmt"
	"slices"
	"sync"
	"time"

	"gourdian/internal/coach"
	"gourdian/internal/gsi"
	"gourdian/internal/stats"
)

const (
	// pickMinGames is how many matches a hero needs before its record says anything.
	pickMinGames = 3
	pickShow     = 4
	pickGoodPct  = 50
	pickAvoidPct = 40
	pickRecent   = 120 * 24 * time.Hour
)

// pickCache keeps pick help, which the draft asks for every tick, until the match history
// changes (or the day does, since old games drop out of the window).
type pickCache struct {
	mu   sync.Mutex
	key  string
	help *coach.PickHelp
}

// pickHelp is your own record for this position: the heroes worth picking and the ones that
// keep losing. Dota tells the player nothing about the draft, so this is history only.
func (s *Server) pickHelp(role string) *coach.PickHelp {
	key := fmt.Sprintf("%s/%d/%s", role, s.stats.HistoryVersion(), time.Now().Format(time.DateOnly))
	c := &s.picks
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.key != key {
		c.key, c.help = key, s.readPickHelp(role)
	}
	return c.help
}

func (s *Server) readPickHelp(role string) *coach.PickHelp {
	matches, err := s.stats.MatchesWhere(stats.MatchFilter{Role: role, Since: time.Now().Add(-pickRecent), Real: true})
	if err != nil || len(matches) == 0 {
		return nil
	}
	type record struct {
		hero          string
		games, wins   int
		lastPlayed    time.Time
		deaths, lh10  int
		countedLH     int
		countedDeaths int
	}
	byHero := map[int]*record{}
	since := time.Now().Add(-pickRecent)
	for _, m := range matches {
		if !m.Real() || m.Role != role || m.HeroID == 0 || m.EndedAt.Before(since) {
			continue
		}
		r := byHero[m.HeroID]
		if r == nil {
			r = &record{hero: m.Hero}
			byHero[m.HeroID] = r
		}
		r.games++
		if m.Result == "win" {
			r.wins++
		}
		if m.EndedAt.After(r.lastPlayed) {
			r.lastPlayed = m.EndedAt
		}
		if m.Deaths > 0 || m.Result != "" {
			r.deaths += m.Deaths
			r.countedDeaths++
		}
		if lh, ok := m.LastHitsAt["10:00"]; ok {
			r.lh10 += lh
			r.countedLH++
		}
	}
	var best, avoid []coach.HeroRecord
	for _, r := range byHero {
		if r.games < pickMinGames {
			continue
		}
		rec := coach.HeroRecord{Hero: r.hero, Games: r.games, Wins: r.wins, WinPct: r.wins * 100 / r.games}
		if r.countedDeaths > 0 {
			rec.AvgDeaths = float64(r.deaths) / float64(r.countedDeaths)
		}
		if r.countedLH > 0 {
			rec.AvgLH10 = r.lh10 / r.countedLH
		}
		switch {
		case rec.WinPct < pickAvoidPct:
			avoid = append(avoid, rec)
		case rec.WinPct >= pickGoodPct:
			best = append(best, rec)
		}
	}
	if len(best) == 0 && len(avoid) == 0 {
		return nil
	}
	byWins := func(a, b coach.HeroRecord) int {
		if a.WinPct != b.WinPct {
			return b.WinPct - a.WinPct
		}
		return b.Games - a.Games
	}
	slices.SortFunc(best, byWins)
	slices.SortFunc(avoid, func(a, b coach.HeroRecord) int { return byWins(b, a) })
	return &coach.PickHelp{
		Role:  role,
		Best:  best[:min(len(best), pickShow)],
		Avoid: avoid[:min(len(avoid), 2)],
	}
}

// pickMatters reports whether the player is still choosing a hero: Dota says it's the draft
// and has no hero for them yet. (It used to ask for a match in progress without a hero, which
// never happens, so pick help never showed.)
func pickMatters(snap coach.Snapshot) bool {
	if !snap.Connected || snap.Hero != nil {
		return false
	}
	switch snap.GameState {
	case gsi.StateWaitForPlayers, gsi.StateHeroSelection, gsi.StateStrategyTime:
		return true
	}
	return false
}
