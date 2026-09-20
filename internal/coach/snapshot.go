package coach

import (
	"cmp"
	"fmt"
	"slices"
	"time"

	"gourdian/internal/config"
	"gourdian/internal/dota"
	"gourdian/internal/dotadata"
	"gourdian/internal/gsi"
	"gourdian/internal/model"
	"gourdian/internal/picks"
)

const connectedWindow = 35 * time.Second

// dayNightWithin is how soon the day or night turn has to be for the HUD to show a timer for it.
const dayNightWithin = 60

type Snapshot struct {
	Connected  bool        `json:"connected"`
	LastUpdate time.Time   `json:"last_update"`
	InMatch    bool        `json:"in_match"`
	GameState  string      `json:"game_state,omitempty"`
	MatchID    string      `json:"match_id,omitempty"`
	Clock      int         `json:"clock"`
	Paused     bool        `json:"paused"`
	Daytime    bool        `json:"daytime"`
	Team       string      `json:"team,omitempty"`
	Hero       *HeroView   `json:"hero,omitempty"`
	Player     *gsi.Player `json:"player,omitempty"`
	Pace       *Pace       `json:"pace,omitempty"`
	Items      []ItemView  `json:"items,omitempty"`
	Build      []BuildView `json:"build,omitempty"`
	Skill      *SkillView  `json:"skill,omitempty"`
	// Sources says where the build, skill order, targets and timers come from.
	Sources   []Source       `json:"sources,omitempty"`
	Timers    []Timer        `json:"timers,omitempty"`
	Focus     string         `json:"focus,omitempty"`
	ItemGoals []ItemGoalView `json:"item_goals,omitempty"`
	// Picks and Briefing are filled in by the trainer, before the pick and before the horn.
	Picks    *picks.Board `json:"picks,omitempty"`
	Drill    *DrillView   `json:"drill,omitempty"`
	Briefing *Briefing    `json:"briefing,omitempty"`
	Role     string       `json:"role,omitempty"`
	// RoleNote says where the role came from, such as a guess from the hero.
	RoleNote string `json:"role_note,omitempty"`
}

type HeroView struct {
	ID              int    `json:"id"`
	Name            string `json:"name"`
	Img             string `json:"img,omitempty"`
	Level           int    `json:"level"`
	Alive           bool   `json:"alive"`
	RespawnSeconds  int    `json:"respawn_seconds"`
	Health          int    `json:"health"`
	MaxHealth       int    `json:"max_health"`
	HealthPercent   int    `json:"health_percent"`
	Mana            int    `json:"mana"`
	MaxMana         int    `json:"max_mana"`
	ManaPercent     int    `json:"mana_percent"`
	BuybackCost     int    `json:"buyback_cost"`
	BuybackCooldown int    `json:"buyback_cooldown"`
}

type Pace struct {
	LastHits   int    `json:"last_hits"`
	Expected   int    `json:"expected"`
	Checkpoint string `json:"checkpoint,omitempty"`
	Target     int    `json:"target,omitempty"`
	Usual      int    `json:"usual,omitempty"` // the player's median at the checkpoint
}

// Briefing sums up the player's plan for a match before the horn.
type Briefing struct {
	Hero       string               `json:"hero"`
	Role       string               `json:"role"`
	Games      int                  `json:"games"` // real matches on this hero and position
	Wins       int                  `json:"wins"`
	Target10   int                  `json:"target_10,omitempty"`
	Usual10    int                  `json:"usual_10,omitempty"`
	Items      []ItemGoal           `json:"items,omitempty"`
	Goals      []model.GoalProgress `json:"goals,omitempty"`
	LastReview string               `json:"last_review,omitempty"` // focus from the last review on this hero
}

// DrillView is the one habit the player is working on, and how it is going this match.
type DrillView struct {
	Rule  string `json:"rule"`
	Label string `json:"label"`
	Count int    `json:"count"`
}

type ItemGoalView struct {
	ItemGoal
	Owned     bool `json:"owned"`
	Remaining int  `json:"remaining"`
	At        int  `json:"at,omitempty"` // when it was bought, if seen
}

type ItemView struct {
	Slot     string `json:"slot"`
	Name     string `json:"name"`
	DName    string `json:"dname"`
	Img      string `json:"img,omitempty"`
	Charges  int    `json:"charges,omitempty"`
	Cooldown int    `json:"cooldown,omitempty"`
}

type BuildView struct {
	dotadata.BuildItem
	Owned     bool `json:"owned"`
	Skipped   bool `json:"skipped"`
	Remaining int  `json:"remaining"`
	Next      bool `json:"next"`
}

type Timer struct {
	Label string `json:"label"`
	At    int    `json:"at"`
	Kind  string `json:"kind"`
}

var itemSlots = []string{"slot0", "slot1", "slot2", "slot3", "slot4", "slot5", "slot6", "slot7", "slot8",
	"teleport0", "neutral0", "stash0", "stash1", "stash2", "stash3", "stash4", "stash5"}

// ruleLabel is a rule's name in the player's language, for showing its drill.
func (e *Engine) ruleLabel(id string) string {
	for _, r := range e.rules {
		if r.ID == id {
			return r.Label
		}
	}
	return id
}

func (e *Engine) Snapshot(set config.Settings) Snapshot {
	targets := e.targetsFor(e.HeroID(), set.Role)
	e.mu.Lock()
	defer e.mu.Unlock()
	snap := Snapshot{LastUpdate: e.lastSeen, Connected: !e.lastSeen.IsZero() && e.now().Sub(e.lastSeen) < connectedWindow, Focus: e.focus,
		Role: set.Role, RoleNote: e.roleNote}
	s := e.last
	if s == nil || s.Map == nil {
		return snap
	}
	snap.GameState, snap.MatchID, snap.Clock = s.Map.GameState, s.Map.MatchID, s.Map.ClockTime
	if set.Drill != "" && e.match != nil {
		snap.Drill = &DrillView{Rule: set.Drill, Label: e.ruleLabel(set.Drill), Count: e.match.ruleFires[set.Drill]}
	}
	if e.match != nil && e.match.gsiID == s.Map.MatchID {
		snap.MatchID = e.match.id
	}
	snap.Paused, snap.Daytime = s.Map.Paused, s.Map.Daytime
	snap.InMatch = s.InMatch()
	if s.Player != nil {
		snap.Player, snap.Team = s.Player, s.Player.TeamName
	}
	if s.Hero == nil || s.Hero.ID == 0 {
		return snap
	}
	h := s.Hero
	snap.Hero = &HeroView{
		ID: h.ID, Name: e.heroName(h), Level: h.Level, Alive: h.Alive, RespawnSeconds: h.RespawnSeconds,
		Health: h.Health, MaxHealth: h.MaxHealth, HealthPercent: h.HealthPercent,
		Mana: h.Mana, MaxMana: h.MaxMana, ManaPercent: h.ManaPercent,
		BuybackCost: h.BuybackCost, BuybackCooldown: h.BuybackCooldown,
	}

	var items map[string]dotadata.ItemInfo
	var build *dotadata.Build
	if e.data != nil {
		items, build = e.data.Items(), e.data.BuildFor(h.ID, set.Role)
		if info, ok := e.data.Hero(h.ID); ok {
			snap.Hero.Img = info.Img
		}
		points := 0
		if e.match != nil {
			points = e.match.skillSpare()
		}
		snap.Skill = skillView(e.data, s, set.Role, set.Language, points)
	}
	for _, slot := range itemSlots {
		it, ok := s.Items[slot]
		if !ok || it.Empty() {
			continue
		}
		v := ItemView{Slot: slot, Name: it.Short(), DName: it.Short(), Charges: it.Charges, Cooldown: it.Cooldown}
		if info, ok := items[it.Short()]; ok {
			v.DName, v.Img = info.DName, info.Img
		}
		snap.Items = append(snap.Items, v)
	}

	if build != nil && items != nil {
		held := heldNames(s)
		p := build.Progress(held, items, s.Map.ClockTime)
		for i, it := range build.Items {
			v := BuildView{BuildItem: it, Owned: p.Owned[i], Skipped: p.Skipped[i], Next: i == p.Next}
			if !v.Owned {
				v.Remaining = dotadata.RemainingCost(it.Name, held, items)
			}
			snap.Build = append(snap.Build, v)
		}
	}

	if s.Player != nil {
		if exp, ok := expectedLastHits(targets.LastHits, s.Map.ClockTime); ok {
			snap.Pace = &Pace{LastHits: s.Player.LastHits, Expected: exp}
			for i, cp := range paceCheckpoints {
				if cp > s.Map.ClockTime {
					snap.Pace.Checkpoint, snap.Pace.Target = dota.Clock(cp), targets.LastHits[i]
					if i < len(targets.Usual) {
						snap.Pace.Usual = targets.Usual[i]
					}
					break
				}
			}
		}
	}
	if items != nil {
		held := heldNames(s)
		owned := dotadata.OwnedClosure(held, items)
		for _, g := range targets.Items {
			v := ItemGoalView{ItemGoal: g, Owned: owned[g.Item]}
			if !v.Owned {
				v.Remaining = dotadata.RemainingCost(g.Item, held, items)
			}
			if e.match != nil {
				if at, ok := e.match.itemSeen[g.Item]; ok && at >= 0 {
					v.At = at
				}
			}
			snap.ItemGoals = append(snap.ItemGoals, v)
		}
	}
	snap.Timers = timers(s.Map.ClockTime, s.Map.Daytime, set, e.match)
	var skills *dotadata.SkillBuild
	if e.data != nil {
		skills = e.data.SkillBuildFor(h.ID, set.Role)
	}
	snap.Sources = sources(build, skills, targets, set.Timings, snap.Hero.Name, set.Role, set.Language)
	return snap
}

func timers(clock int, daytime bool, set config.Settings, m *match) []Timer {
	t := set.Timings
	var out []Timer
	add := func(label, kind string, at int) {
		if at >= clock {
			out = append(out, Timer{Label: label, At: at, Kind: kind})
		}
	}
	periodic := func(label string, first, period int) {
		if at, ok := nextPeriodic(clock, first, period); ok {
			add(label, "rune", at)
		}
	}
	periodic("Bounty runes", 0, t.BountyRuneEvery)
	for _, w := range t.WaterRunes {
		if w >= clock {
			add("Water runes", "rune", w)
			break
		}
	}
	periodic("Power rune", t.PowerRuneFirst, t.PowerRuneEvery)
	periodic("Shrines of Wisdom", t.WisdomRuneEvery, t.WisdomRuneEvery)
	for i, tier := range t.NeutralTiers {
		if tier >= clock {
			add(fmt.Sprintf("Neutral tier %d", i+1), "neutral", tier)
			break
		}
	}
	if t.TormentorSpawn > 0 {
		add("Tormentor", "objective", t.TormentorSpawn)
	}
	// Day and night turn over every five minutes, so a timer for it all game would cost a line
	// most of the time; it appears as the turn comes close, which is when it changes what you do.
	if at, ok := nextPeriodic(clock, t.DayNightEvery, t.DayNightEvery); ok && clock >= 0 && at-clock <= dayNightWithin {
		label := "Night falls"
		if !daytime {
			label = "Day breaks"
		}
		add(label, "daynight", at)
	}
	if m != nil && m.roshanKnown {
		lo, hi := m.roshanDeadAt+t.RoshanRespawnMin, m.roshanDeadAt+t.RoshanRespawnMax
		if clock < lo {
			add("Roshan window opens", "objective", lo)
		} else {
			add("Roshan surely up", "objective", hi)
		}
	}
	if m != nil && m.aegisKnown && !m.aegisUsed {
		label := "Aegis" + teamSuffix(m.aegisTeam, m.team, "on") + " expires"
		if m.aegisMine {
			label = "Your Aegis expires"
		}
		add(label, "objective", m.aegisExpires)
	}
	if slices.Contains(supports, set.Role) && clock >= 0 {
		pull := clock - clock%60 + 53
		if clock%60 > 53 {
			pull += 60
		}
		add("Stack pull", "stack", pull)
	}
	slices.SortStableFunc(out, func(a, b Timer) int { return cmp.Compare(a.At, b.At) })
	return out[:min(len(out), 7)]
}
