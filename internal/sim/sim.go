// Package sim replays a scripted Anti-Mage match against the trainer, with deliberate
// mistakes so every kind of tip can be seen without launching Dota.
package sim

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"iter"
	"math"
	"math/rand/v2"
	"net/http"
	"slices"
	"time"

	"gourdian/internal/coach"
	"gourdian/internal/gsi"
)

type Options struct {
	URL   string
	Token string
	Speed float64
	From  int
	To    int
	// Seed varies hero, farm speed, extra deaths and the result; 0 plays the fixed script.
	Seed uint64
}

var simHeroes = []struct {
	id   int
	name string
}{{1, "npc_dota_hero_antimage"}, {8, "npc_dota_hero_juggernaut"}, {44, "npc_dota_hero_phantom_assassin"}, {94, "npc_dota_hero_medusa"}}

const gameTimeOffset = 95

type game struct {
	matchID string
	clock   int

	heroID      int
	heroName    string
	farmEvery   [2]int
	extraDeaths []int
	win         bool
	laneAt      int

	gold, earned                             int
	lastHits, denies, kills, deaths, assists int
	level                                    int
	alive                                    bool
	respawnAt                                int
	x, y                                     int
	abilities                                []gsi.Ability
	talents                                  [8]bool
	items                                    map[string]gsi.Item
	events                                   []gsi.Event
}

func Run(ctx context.Context, o Options, progress func(clock int, status int)) error {
	interval := time.Duration(float64(time.Second) / o.Speed)
	client := &http.Client{Timeout: 5 * time.Second}
	tick := time.NewTicker(interval)
	defer tick.Stop()
	for clock, s := range States(o) {
		status, err := post(ctx, client, o, s)
		if err != nil {
			return err
		}
		if progress != nil {
			progress(clock, status)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-tick.C:
		}
	}
	return nil
}

// States are the game states of the scripted match o describes, one per game second with the
// second it stands for, as Dota would post them; Run sends them to a trainer, and benchmarks
// feed them to the rules. The clock stops at the end of the match, as Dota's does, so the
// last few states repeat it.
func States(o Options) iter.Seq2[int, *gsi.State] {
	return func(yield func(int, *gsi.State) bool) {
		g := newGame(o)
		for clock := o.From; clock <= o.To+3; clock++ {
			state := gsi.StateInProgress
			if clock < 0 {
				state = gsi.StatePreGame
			}
			if clock > o.To {
				state = gsi.StatePostGame
			} else {
				g.step(clock)
			}
			if !yield(clock, g.snapshot(state)) {
				return
			}
		}
	}
}

func newGame(o Options) *game {
	g := &game{
		matchID:   fmt.Sprintf("sim-%d", time.Now().UnixNano()),
		gold:      600,
		level:     1,
		alive:     true,
		items:     map[string]gsi.Item{},
		heroID:    simHeroes[0].id,
		heroName:  simHeroes[0].name,
		farmEvery: [2]int{11, 5},
		win:       true,
		laneAt:    -1,
		abilities: []gsi.Ability{
			{Name: "antimage_mana_break", Passive: true},
			{Name: "antimage_blink"},
			{Name: "antimage_counterspell"},
			{Name: "antimage_persectur", Level: 1, Passive: true},
			{Name: "antimage_mana_void", Ultimate: true},
		},
	}
	for _, slot := range []string{"slot0", "slot1", "slot2", "slot3", "slot4", "slot5", "slot6", "slot7", "slot8",
		"stash0", "stash1", "stash2", "stash3", "stash4", "stash5", "teleport0", "neutral0"} {
		g.items[slot] = gsi.Item{Name: "empty"}
	}
	g.x, g.y = -7000, -6500
	if o.Seed != 0 {
		rng := rand.New(rand.NewPCG(o.Seed, o.Seed>>32))
		h := simHeroes[rng.IntN(len(simHeroes))]
		g.heroID, g.heroName = h.id, h.name
		g.farmEvery = [2]int{8 + rng.IntN(7), 4 + rng.IntN(4)}
		for range rng.IntN(7) {
			g.extraDeaths = append(g.extraDeaths, 620+rng.IntN(max(o.To-700, 1)))
		}
		g.win = rng.IntN(2) == 0
	}
	return g
}

func post(ctx context.Context, client *http.Client, o Options, s *gsi.State) (int, error) {
	s.Auth = &gsi.Auth{Token: o.Token}
	body, err := json.Marshal(s)
	if err != nil {
		return 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.URL, bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("is the trainer running? %w", err)
	}
	resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		return resp.StatusCode, fmt.Errorf("trainer rejected the auth token")
	}
	return resp.StatusCode, nil
}

func (g *game) put(slot, name string, charges int) {
	g.items[slot] = gsi.Item{Name: "item_" + name, CanCast: true, Charges: charges}
}

func (g *game) clear(slot string) { g.items[slot] = gsi.Item{Name: "empty"} }

func (g *game) buy(cost int) { g.gold = max(g.gold-cost, 0) }

func (g *game) step(clock int) {
	g.clock = clock
	idle := clock >= 960 && clock < 1000
	if g.alive && slices.Contains(g.extraDeaths, clock) {
		g.die(clock)
	}
	if !g.alive && clock >= g.respawnAt {
		g.alive, g.laneAt = true, clock+24
		g.x, g.y = -7100, -6600
	}
	if clock == g.laneAt {
		g.x, g.y = 4200, -6200
	}

	switch clock {
	case -50:
		g.put("slot0", "tango", 3)
		g.put("slot1", "flask", 1)
		g.put("slot2", "quelling_blade", 0)
		g.put("slot3", "branches", 0)
		g.put("slot4", "branches", 0)
		g.put("teleport0", "tpscroll", 1)
		g.buy(465)
	case 0:
		g.x, g.y = 4200, -6200
	case 150:
		g.clear("teleport0")
	case 180:
		g.clear("slot3")
		g.clear("slot4")
		g.put("slot3", "magic_wand", 12)
		g.buy(250)
	case 220:
		g.put("teleport0", "tpscroll", 1)
		g.buy(100)
	case 260:
		g.put("stash0", "boots", 0)
		g.buy(500)
	case 290:
		g.clear("stash0")
		g.put("slot4", "boots", 0)
	case 360:
		g.clear("slot4")
		g.put("slot4", "power_treads", 0)
		g.buy(900)
	case 490:
		g.die(clock)
	case 600:
		g.put("neutral0", "occult_bracelet", 0)
	case 840:
		g.clear("slot3")
		g.put("slot6", "magic_wand", 14)
	case 880:
		g.clear("slot6")
		g.put("slot3", "magic_wand", 15)
	case 1080:
		g.events = append(g.events,
			gsi.Event{GameTime: clock + gameTimeOffset, EventType: "roshan_killed", Team: "radiant", KilledByTeam: "radiant"},
			gsi.Event{GameTime: clock + 2 + gameTimeOffset, EventType: "aegis_picked_up", PlayerID: 0, Team: "radiant"})
	}
	if clock >= 750 && g.gold >= 3900 && g.items["slot5"].Empty() {
		g.put("slot5", "bfury", 0)
		g.buy(3900)
	}

	if clock > 0 && g.alive && !idle {
		every := g.farmEvery[0]
		if clock >= 600 {
			every = g.farmEvery[1]
		}
		if clock%every == 0 {
			g.lastHits++
			g.gold += 42
			g.earned += 42
		}
		if clock%37 == 0 {
			g.denies++
		}
		g.x += int(40 * math.Sin(float64(clock)/7))
		g.y += int(40 * math.Cos(float64(clock)/9))
	}
	if clock > 0 {
		g.gold += 2
		g.earned += 2
	}
	if clock == 1200 || clock == 1400 {
		g.kills++
		g.gold += 300
		g.earned += 300
	}
	if clock%240 == 100 {
		g.assists++
	}

	g.level = min(25, 1+max(clock, 0)/75)
	levelSixDelay := g.level == 6 && clock < 375+30
	for !levelSixDelay && g.spent() < coach.SkillPointsAtLevel(g.level) {
		if !g.spendPoint() {
			break
		}
	}
}

func (g *game) die(clock int) {
	g.alive, g.respawnAt, g.deaths = false, clock+26, g.deaths+1
	g.gold = max(g.gold-250, 0)
}

func (g *game) spent() int {
	n := 0
	for _, a := range g.abilities {
		if a.Name != "antimage_persectur" {
			n += a.Level
		}
	}
	for _, t := range g.talents {
		if t {
			n++
		}
	}
	return n
}

func (g *game) spendPoint() bool {
	due := 0
	for _, l := range []int{10, 15, 20, 25} {
		if g.level >= l {
			due++
		}
	}
	taken := 0
	for _, t := range g.talents {
		if t {
			taken++
		}
	}
	if taken < due {
		g.talents[taken*2] = true
		return true
	}
	ult := &g.abilities[4]
	if ult.Level < min(3, g.level/6) {
		ult.Level++
		return true
	}
	best := -1
	for i := range 3 {
		if g.abilities[i].Level < 4 && (best < 0 || g.abilities[i].Level < g.abilities[best].Level) {
			best = i
		}
	}
	if best < 0 {
		return false
	}
	g.abilities[best].Level++
	return true
}

func (g *game) snapshot(state string) *gsi.State {
	clock := g.clock
	maxHP := 640 + 22*g.level
	hpPct := 85 + int(15*math.Sin(float64(clock)/13))
	switch {
	case clock >= 440 && clock < 455:
		hpPct = 18
	case !g.alive:
		hpPct = 0
	case g.laneAt > 0 && clock < g.laneAt:
		hpPct = 100
	}
	maxMana := 300 + 12*g.level
	manaPct := 70
	if g.laneAt > 0 && clock < g.laneAt {
		manaPct = 100
	}
	respawn := 0
	if !g.alive {
		respawn = g.respawnAt - clock
	}
	gpm, xpm := 0, 0
	if clock > 0 {
		gpm = g.earned * 60 / clock
		xpm = 420 + clock/10
	}
	winTeam := "none"
	if state == gsi.StatePostGame {
		winTeam = "radiant"
		if !g.win {
			winTeam = "dire"
		}
	}

	s := &gsi.State{
		Provider: &gsi.Provider{Name: "Dota 2", AppID: 570, Version: 47, Timestamp: time.Now().Unix()},
		Map: &gsi.Map{
			Name: "start", MatchID: g.matchID, GameTime: clock + gameTimeOffset, ClockTime: clock,
			Daytime: (max(clock, 0)/300)%2 == 0, GameState: state, WinTeam: winTeam,
			RadiantScore: g.kills + 3, DireScore: g.deaths + 2,
		},
		Player: &gsi.Player{
			SteamID: "76561190000000000", Name: "Simulated Player", Activity: "playing",
			Kills: g.kills, Deaths: g.deaths, Assists: g.assists, LastHits: g.lastHits, Denies: g.denies,
			TeamName: "radiant", Gold: g.gold, GoldReliable: g.gold / 4, GoldUnreliable: g.gold - g.gold/4, GPM: gpm, XPM: xpm,
		},
		Hero: &gsi.Hero{
			ID: g.heroID, Name: g.heroName, Level: g.level, XPos: g.x, YPos: g.y,
			Alive: g.alive, RespawnSeconds: respawn, BuybackCost: 200 + g.level*60, BuybackCooldown: 0,
			Health: maxHP * hpPct / 100, MaxHealth: maxHP, HealthPercent: hpPct,
			Mana: maxMana * manaPct / 100, MaxMana: maxMana, ManaPercent: manaPct,
			Talent1: g.talents[0], Talent2: g.talents[1], Talent3: g.talents[2], Talent4: g.talents[3],
			Talent5: g.talents[4], Talent6: g.talents[5], Talent7: g.talents[6], Talent8: g.talents[7],
		},
		Abilities: map[string]gsi.Ability{},
		Items:     map[string]gsi.Item{},
		Events:    g.events,
	}
	for i, a := range g.abilities {
		a.CanCast = a.Level > 0 && !a.Passive
		s.Abilities[fmt.Sprintf("ability%d", i)] = a
	}
	for k, v := range g.items {
		s.Items[k] = v
	}
	return s
}
