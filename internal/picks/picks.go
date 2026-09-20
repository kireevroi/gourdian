// Package picks ranks the heroes worth choosing in a position. It scores each hero in
// percentage points around an even game, so every part of the score can be printed as the
// reason for it, and it imports only the trainer's vocabulary and data, never its server.
package picks

import (
	"cmp"
	"fmt"
	"math"
	"slices"
	"time"

	"gourdian/internal/dota"
	"gourdian/internal/dotadata"
	"gourdian/internal/model"
)

const (
	// MinGames is how many matches on a hero in a position make a record worth ranking.
	MinGames = 3
	// Window is how far back the player's history is read.
	Window = 120 * 24 * time.Hour
	// halfLife is how long it takes a match to count half as much as a fresh one.
	halfLife = 90 * 24 * time.Hour
	// rustyAfter is how long since the last game before a hero is called rusty.
	rustyAfter = 30 * 24 * time.Hour

	// avoidPct is the win rate below which a hero is listed as one to avoid.
	avoidPct = 40
	// freshPct is the meta win rate a hero the player doesn't play must beat to be suggested.
	freshPct = 50

	// Show is how many heroes each list holds.
	Show      = 4
	ShowAvoid = 2
	ShowFresh = 2

	// yoursK is the prior, in games, that a record is shrunk towards an even one. It is what
	// keeps three games at 100% from outranking forty at 65%.
	yoursK   = 20.0
	yoursCap = 15
	metaCap  = 6
	fitBonus = 3

	// even is the score of a hero with nothing for or against it.
	even = 50
)

// Board is the pick advice for one position.
type Board struct {
	Role string `json:"role"`
	// Best is the player's own pool, ranked. Fresh is heroes they don't play that the meta
	// likes, kept apart so nothing ever tells them to first-pick a hero they've never played.
	Best  []Hero `json:"best,omitempty"`
	Fresh []Hero `json:"fresh,omitempty"`
	Avoid []Hero `json:"avoid,omitempty"`
}

// Empty reports whether the board has nothing to show.
func (b *Board) Empty() bool {
	return b == nil || len(b.Best)+len(b.Fresh)+len(b.Avoid) == 0
}

type Hero struct {
	ID   int    `json:"id,omitempty"`
	Name string `json:"hero"`
	Img  string `json:"img,omitempty"`
	// Score is the hero's chances in percentage points: 50 is an even game.
	Score int `json:"score"`
	// Games, Wins and WinPct are the player's plain record over Window, whatever the score
	// makes of it.
	Games     int      `json:"games,omitempty"`
	Wins      int      `json:"wins,omitempty"`
	WinPct    int      `json:"win_pct,omitempty"`
	AvgDeaths float64  `json:"avg_deaths,omitempty"`
	AvgLH10   int      `json:"avg_lh10,omitempty"`
	Why       []string `json:"why,omitempty"`
}

// Input is everything a board is made from. History is the player's matches, already filtered
// to the position or not; Rank is their OpenDota rank tier, 0 when unknown; Meta and Heroes are
// nil until OpenDota answers, and the board is made without them.
type Input struct {
	Role    string
	Lang    string
	Rank    int
	History []model.MatchSummary
	Heroes  []dotadata.HeroInfo
	Meta    map[int]dotadata.HeroMeta
}

// words formats a reason in the player's language, the way coach.sources does.
type words string

func (w words) f(en, ru string, args ...any) string {
	if w == "ru" {
		return fmt.Sprintf(ru, args...)
	}
	return fmt.Sprintf(en, args...)
}

// record is one hero's history in the position.
type record struct {
	id          int
	name        string
	games, wins int
	// weight and weighted are the same games counted by how recent they are.
	weight, weighted   float64
	last               time.Time
	deaths, deathGames int
	lh10, lh10Games    int
}

// Rank scores every hero the player has a record on, plus the ones the meta likes that they
// don't play, and returns nil when there is nothing worth saying.
func Rank(in Input, now time.Time) *Board {
	w := words(in.Lang)
	bracket := dota.Bracket(in.Rank)
	byHero := gather(in, now)
	info := make(map[int]dotadata.HeroInfo, len(in.Heroes))
	for _, h := range in.Heroes {
		info[h.ID] = h
	}

	b := &Board{Role: in.Role}
	for _, r := range byHero {
		if r.games < MinGames {
			continue
		}
		h := Hero{ID: r.id, Name: r.name, Games: r.games, Wins: r.wins, WinPct: r.wins * 100 / r.games}
		if r.deathGames > 0 {
			h.AvgDeaths = float64(r.deaths) / float64(r.deathGames)
		}
		if r.lh10Games > 0 {
			h.AvgLH10 = r.lh10 / r.lh10Games
		}
		h.Img = info[r.id].Img
		meta := in.Meta[r.id]
		h.Score = even + yours(r, &h, w) + metaTerm(meta, bracket, &h, w) + fit(meta.Roles, in.Role)
		// A hero fades by a point for every rustyAfter since it was last played. The recency
		// weighting already counts its games for less; this is what the player is told.
		if rust := now.Sub(r.last); rust > rustyAfter {
			h.Score -= int(rust / rustyAfter)
			h.Why = append(h.Why, w.f("rusty, %d days", "не играли %d дн.", int(rust/(24*time.Hour))))
		}
		if h.WinPct < avoidPct {
			b.Avoid = append(b.Avoid, h)
		} else {
			b.Best = append(b.Best, h)
		}
	}
	slices.SortFunc(b.Best, byScore)
	slices.SortFunc(b.Avoid, func(a, c Hero) int { return byScore(c, a) })
	b.Best = b.Best[:min(len(b.Best), Show)]
	b.Avoid = b.Avoid[:min(len(b.Avoid), ShowAvoid)]
	b.Fresh = fresh(in, byHero, bracket, w)
	if b.Empty() {
		return nil
	}
	return b
}

func byScore(a, b Hero) int {
	return cmp.Or(b.Score-a.Score, b.Games-a.Games, cmp.Compare(a.Name, b.Name))
}

// gather sums the player's matches in the position, each weighted by how recent it is.
func gather(in Input, now time.Time) map[int]*record {
	since := now.Add(-Window)
	byHero := map[int]*record{}
	for _, m := range in.History {
		if !m.Real() || m.HeroID == 0 || m.Role != in.Role || m.EndedAt.Before(since) {
			continue
		}
		r := byHero[m.HeroID]
		if r == nil {
			r = &record{id: m.HeroID, name: m.Hero}
			byHero[m.HeroID] = r
		}
		weight := math.Pow(0.5, now.Sub(m.EndedAt).Seconds()/halfLife.Seconds())
		r.games++
		r.weight += weight
		if m.Result == "win" {
			r.wins++
			r.weighted += weight
		}
		if m.EndedAt.After(r.last) {
			r.last = m.EndedAt
		}
		if m.Deaths > 0 || m.Result != "" {
			r.deaths += m.Deaths
			r.deathGames++
		}
		if lh, ok := m.LastHitsAt["10:00"]; ok {
			r.lh10 += lh
			r.lh10Games++
		}
	}
	return byHero
}

// yours is what the player's own record is worth, shrunk towards an even game by how few
// games it rests on, so a short hot streak can't outrank a long steady record.
func yours(r *record, h *Hero, w words) int {
	if r.weight == 0 {
		return 0
	}
	rate := r.weighted / r.weight * 100
	term := clamp(int(math.Round((rate-even)*(r.weight/(r.weight+yoursK)))), yoursCap)
	h.Why = append(h.Why, w.f("your %d games %d%%", "ваших матчей: %d, %d%%", r.games, h.WinPct))
	return term
}

// metaTerm is how the hero is doing in public games at the player's own bracket.
func metaTerm(m dotadata.HeroMeta, bracket int, h *Hero, w words) int {
	pct, games := m.WinPct(bracket)
	if games == 0 {
		return 0
	}
	where := dota.MedalName(bracket, string(w))
	if where == "" {
		h.Why = append(h.Why, w.f("meta %d%%", "в мете %d%%", pct))
	} else {
		h.Why = append(h.Why, w.f("meta %d%% at %s", "в мете %d%% (%s)", pct, where))
	}
	return clamp(pct-even, metaCap)
}

// roleFit is the OpenDota roles that suit each position.
var roleFit = map[string][]string{
	dota.Carry:       {"Carry"},
	dota.Mid:         {"Carry", "Nuker"},
	dota.Offlane:     {"Initiator", "Durable"},
	dota.SoftSupport: {"Support", "Initiator"},
	dota.HardSupport: {"Support"},
}

// fit nudges a hero by how well OpenDota's roles suit the position. It stays small: the
// player's own record already knows far more about what suits them than a role tag does.
func fit(roles []string, role string) int {
	switch {
	case dota.Core(role) && slices.Contains(roles, "Support"):
		return -fitBonus
	case slices.ContainsFunc(roles, func(r string) bool { return slices.Contains(roleFit[role], r) }):
		return fitBonus
	}
	return 0
}

// fresh is the heroes the player doesn't play that are doing well at their bracket. They are
// kept in their own list, never mixed into the ranked pool.
func fresh(in Input, played map[int]*record, bracket int, w words) []Hero {
	if len(in.Meta) == 0 {
		return nil
	}
	var out []Hero
	for _, info := range in.Heroes {
		if r := played[info.ID]; r != nil && r.games >= MinGames {
			continue
		}
		m, ok := in.Meta[info.ID]
		if !ok {
			continue
		}
		pct, games := m.WinPct(bracket)
		if games == 0 || pct < freshPct {
			continue
		}
		h := Hero{ID: info.ID, Name: info.LocalizedName, Img: info.Img}
		h.Score = even + metaTerm(m, bracket, &h, w) + fit(m.Roles, in.Role)
		out = append(out, h)
	}
	slices.SortFunc(out, byScore)
	return out[:min(len(out), ShowFresh)]
}

func clamp(v, limit int) int { return max(-limit, min(limit, v)) }
