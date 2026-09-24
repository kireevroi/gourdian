// Package picks ranks heroes for a position in percentage points around an even game.
package picks

import (
	"cmp"
	"fmt"
	"math"
	"slices"
	"time"

	"gourdian/internal/data/opendota"
	"gourdian/internal/data/stratz"
	"gourdian/internal/game/dota"
	"gourdian/internal/game/model"
)

const day = 24 * time.Hour

type Tuning struct {
	// HalfLifeDays, not Days, decides how much an old game counts.
	Days         int `json:"days"`
	HalfLifeDays int `json:"half_life_days"`
	// TrustAfter is the game count at which a hero's record counts for half of what it says.
	TrustAfter int `json:"trust_after"`
	MinGames   int `json:"min_games"`
	AvoidPct   int `json:"avoid_pct"`
	// Fresh 0 turns off heroes the player doesn't play.
	Show  int `json:"show"`
	Fresh int `json:"fresh"`
	Avoid int `json:"avoid"`
}

const MatchupMinGames = 200

func DefaultTuning() Tuning {
	return Tuning{Days: 365, HalfLifeDays: 45, TrustAfter: 20, MinGames: 3, AvoidPct: 40, Show: 4, Fresh: 2, Avoid: 2}
}

func (t Tuning) Window() time.Duration { return day * time.Duration(t.Days) }

func (t Tuning) halfLife() time.Duration { return day * time.Duration(t.HalfLifeDays) }

func (t Tuning) rustyAfter() time.Duration { return t.halfLife() }

func (t Tuning) Validate() error {
	switch {
	case t.Days < 7 || t.Days > 5*365:
		return fmt.Errorf("pick history must cover 7 to %d days", 5*365)
	case t.HalfLifeDays < 1 || t.HalfLifeDays > t.Days:
		return fmt.Errorf("pick half-life must be 1 day to the whole history (%d days)", t.Days)
	case t.TrustAfter < 0 || t.TrustAfter > 200:
		return fmt.Errorf("pick records must be trusted after 0 to 200 games")
	case t.MinGames < 1 || t.MinGames > 50:
		return fmt.Errorf("a hero needs 1 to 50 games to be ranked")
	case t.AvoidPct < 0 || t.AvoidPct > 50:
		return fmt.Errorf("the avoid line must be between 0%% and 50%%")
	case t.Show < 0 || t.Show > 10 || t.Fresh < 0 || t.Fresh > 10 || t.Avoid < 0 || t.Avoid > 10:
		return fmt.Errorf("each pick list holds 0 to 10 heroes")
	}
	return nil
}

const (
	// freshPct: the roster median is 49.7%, so 52% (the top sixth) is the bar for a stranger.
	freshPct = 52

	// TODO: tune. yoursCap saturates above about 65%, so the best two heroes tie.
	yoursCap = 15
	metaCap  = 6
	fitBonus = 3
	rustyCap = 5

	// counterCap keeps a counter to ordering heroes: it is measured across every position.
	counterCap = 8

	even = 50
)

type Board struct {
	Role string `json:"role"`
	// Fresh is kept apart so nothing tells them to first-pick a hero they've never played.
	Best    []Hero   `json:"best,omitempty"`
	Fresh   []Hero   `json:"fresh,omitempty"`
	Avoid   []Hero   `json:"avoid,omitempty"`
	Enemies []Hero   `json:"enemies,omitempty"`
	Notes   []string `json:"notes,omitempty"`
	// NeedPosition: the trainer asks rather than guess the position from the last game.
	NeedPosition bool `json:"need_position,omitempty"`
}

// WithoutSuggestions keeps the draft and drops the advice, for after the pick or before a position.
func (b *Board) WithoutSuggestions() *Board {
	if b == nil {
		return nil
	}
	left := &Board{Role: b.Role, Enemies: b.Enemies, Notes: b.Notes}
	if left.Empty() {
		return nil
	}
	return left
}

func (b *Board) Empty() bool {
	if b == nil {
		return true
	}
	return !b.NeedPosition && len(b.Best)+len(b.Fresh)+len(b.Avoid)+len(b.Enemies)+len(b.Notes) == 0
}

type Hero struct {
	ID        int      `json:"id,omitempty"`
	Name      string   `json:"hero"`
	Img       string   `json:"img,omitempty"`
	Score     int      `json:"score"`
	Roles     []string `json:"roles,omitempty"`
	Games     int      `json:"games,omitempty"`
	Wins      int      `json:"wins,omitempty"`
	WinPct    int      `json:"win_pct,omitempty"`
	AvgDeaths float64  `json:"avg_deaths,omitempty"`
	AvgLH10   int      `json:"avg_lh10,omitempty"`
	Why       []string `json:"why,omitempty"`
}

// Meta and Heroes are nil until OpenDota answers.
type Input struct {
	Role     string
	Lang     string
	Rank     int
	Tuning   Tuning
	History  []model.MatchSummary
	Heroes   []opendota.HeroInfo
	Meta     map[int]opendota.HeroMeta
	Enemies  []int
	Allies   []int
	Matchups map[int]map[int]stratz.Edge
}

type words string

func (w words) f(en, ru string, args ...any) string {
	if w == "ru" {
		return fmt.Sprintf(ru, args...)
	}
	return fmt.Sprintf(en, args...)
}

func (w words) s(en, ru string) string {
	if w == "ru" {
		return ru
	}
	return en
}

type record struct {
	id          int
	name        string
	games, wins int
	// weighted counts the same games by how recent they are.
	weight, weighted   float64
	last               time.Time
	deaths, deathGames int
	lh10, lh10Games    int
}

func Rank(in Input, now time.Time) *Board {
	// An invalid tuning means a caller didn't fill one in.
	t := in.Tuning
	if t.Validate() != nil {
		t = DefaultTuning()
	}
	w := words(in.Lang)
	bracket := dota.Bracket(in.Rank)
	byHero := gather(in, t, now)
	info := make(map[int]opendota.HeroInfo, len(in.Heroes))
	for _, h := range in.Heroes {
		info[h.ID] = h
	}

	gone := taken(in)
	b := &Board{Role: in.Role}
	for _, r := range byHero {
		if r.games < t.MinGames || gone[r.id] {
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
		h.Score = even + yours(r, &h, t, w) + metaTerm(meta, bracket, &h, w) + fit(meta.Roles, in.Role) +
			counter(in, r.id, info, &h, w)
		if rust := now.Sub(r.last); rust > t.rustyAfter() {
			h.Score -= min(int(rust/t.rustyAfter()), rustyCap)
			h.Why = append(h.Why, w.f("rusty, %d days", "не играли %d дн.", int(rust/day)))
		}
		if h.WinPct < t.AvoidPct {
			b.Avoid = append(b.Avoid, h)
		} else {
			b.Best = append(b.Best, h)
		}
	}
	slices.SortFunc(b.Best, byScore)
	slices.SortFunc(b.Avoid, func(a, c Hero) int { return byScore(c, a) })
	b.Best = b.Best[:min(len(b.Best), t.Show)]
	b.Avoid = b.Avoid[:min(len(b.Avoid), t.Avoid)]
	b.Fresh = fresh(in, t, byHero, gone, info, bracket, w, max(0, t.Show-len(b.Best)))
	for _, id := range in.Enemies {
		b.Enemies = append(b.Enemies, Hero{ID: id, Name: info[id].LocalizedName, Img: info[id].Img,
			Roles: in.Meta[id].Roles})
	}
	b.Notes = notes(in, w)
	if b.Empty() {
		return nil
	}
	return b
}

// Only their side: ours can be a hover, and dropping it hides the hero about to be locked.
func taken(in Input) map[int]bool {
	gone := map[int]bool{}
	for _, id := range in.Enemies {
		gone[id] = true
	}
	return gone
}

func byScore(a, b Hero) int {
	return cmp.Or(b.Score-a.Score, b.Games-a.Games, cmp.Compare(a.Name, b.Name))
}

func gather(in Input, t Tuning, now time.Time) map[int]*record {
	since := now.Add(-t.Window())
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
		weight := math.Pow(0.5, now.Sub(m.EndedAt).Seconds()/t.halfLife().Seconds())
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

// yours shrinks the record towards even by how few games it rests on.
func yours(r *record, h *Hero, t Tuning, w words) int {
	if r.weight == 0 {
		return 0
	}
	rate := r.weighted / r.weight * 100
	term := clamp(int(math.Round((rate-even)*(r.weight/(r.weight+float64(t.TrustAfter))))), yoursCap)
	h.Why = append(h.Why, w.f("your %d games %d%%", "ваших матчей: %d, %d%%", r.games, h.WinPct))
	return term
}

func metaTerm(m opendota.HeroMeta, bracket int, h *Hero, w words) int {
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

func counter(in Input, heroID int, info map[int]opendota.HeroInfo, h *Hero, w words) int {
	if len(in.Enemies) == 0 || len(in.Matchups) == 0 {
		return 0
	}
	total, counted := 0.0, 0
	best, bestID, worst, worstID := 0.0, 0, 0.0, 0
	for _, enemy := range in.Enemies {
		m, ok := in.Matchups[enemy][heroID]
		if !ok || m.Games == 0 {
			continue
		}
		// The edge is the enemy's over this hero.
		edge := -m.Pct * float64(m.Games) / float64(m.Games+MatchupMinGames)
		total += edge
		counted++
		if edge > best {
			best, bestID = edge, enemy
		}
		if edge < worst {
			worst, worstID = edge, enemy
		}
	}
	if counted == 0 {
		return 0
	}
	term := clamp(int(math.Round(total)), counterCap)
	name := func(id int) string {
		if n := info[id].LocalizedName; n != "" {
			return n
		}
		return w.s("their picks", "их пики")
	}
	switch {
	case term > 0:
		h.Why = append(h.Why, w.f("+%d%% against %s", "+%d%% против %s", term, name(bestID)))
	case term < 0:
		h.Why = append(h.Why, w.f("%d%% against %s", "%d%% против %s", term, name(worstID)))
	}
	return term
}

// LeastForShape: with two heroes picked, "nobody can stun" is not a gap.
const LeastForShape = 4

func notes(in Input, w words) []string {
	var out []string
	has := func(ids []int, role string) int {
		n := 0
		for _, id := range ids {
			if slices.Contains(in.Meta[id].Roles, role) {
				n++
			}
		}
		return n
	}
	if len(in.Enemies) >= LeastForShape {
		if n := has(in.Enemies, "Disabler"); n >= 3 {
			out = append(out, w.f("%d of them can stun or hold you", "стан или контроль есть у %d из них", n))
		}
		ranged := 0
		for _, id := range in.Enemies {
			if !in.Meta[id].Melee() {
				ranged++
			}
		}
		if ranged == len(in.Enemies) {
			out = append(out, w.s("Every one of them is ranged", "Все они дальнего боя"))
		}
	}
	if len(in.Allies) >= LeastForShape {
		if has(in.Allies, "Disabler") == 0 {
			out = append(out, w.s("Nobody on your side can stun or hold", "На вашей стороне некому дать контроль"))
		}
		if has(in.Allies, "Durable") == 0 {
			out = append(out, w.s("Nobody on your side can take a beating", "На вашей стороне некому держать урон"))
		}
		melee := 0
		for _, id := range in.Allies {
			if in.Meta[id].Melee() {
				melee++
			}
		}
		if melee == len(in.Allies) {
			out = append(out, w.s("Your whole side is melee", "Вся ваша сторона ближнего боя"))
		}
	}
	return out
}

var roleFit = map[string][]string{
	dota.Carry:       {"Carry"},
	dota.Mid:         {"Carry", "Nuker"},
	dota.Offlane:     {"Initiator", "Durable"},
	dota.SoftSupport: {"Support", "Initiator"},
	dota.HardSupport: {"Support"},
}

// fit stays small: the player's own record knows more than a role tag.
func fit(roles []string, role string) int {
	switch {
	case dota.Core(role) && slices.Contains(roles, "Support"):
		return -fitBonus
	case slices.ContainsFunc(roles, func(r string) bool { return slices.Contains(roleFit[role], r) }):
		return fitBonus
	}
	return 0
}

func fresh(in Input, t Tuning, played map[int]*record, gone map[int]bool, info map[int]opendota.HeroInfo, bracket int, w words, room int) []Hero {
	if len(in.Meta) == 0 || t.Fresh == 0 || room == 0 {
		return nil
	}
	var out []Hero
	for _, hero := range in.Heroes {
		if r := played[hero.ID]; gone[hero.ID] || r != nil && r.games >= t.MinGames {
			continue
		}
		m, ok := in.Meta[hero.ID]
		if !ok {
			continue
		}
		h := Hero{ID: hero.ID, Name: hero.LocalizedName, Img: hero.Img}
		edge := counter(in, hero.ID, info, &h, w)
		// A hard counter earns a stranger its place as much as the meta does.
		if pct, games := m.WinPct(bracket); games == 0 || pct+edge < freshPct {
			continue
		}
		h.Score = even + metaTerm(m, bracket, &h, w) + fit(m.Roles, in.Role) + edge
		out = append(out, h)
	}
	slices.SortFunc(out, byScore)
	return out[:min(len(out), t.Fresh, room)]
}

func clamp(v, limit int) int { return max(-limit, min(limit, v)) }
