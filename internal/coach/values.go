package coach

import (
	"math"
	"strings"

	"gourdian/internal/dotadata"
	"gourdian/internal/gsi"
)

const (
	idleMoved  = 500
	idleFromMS = 600
)

func readyHeal(c *Ctx) string {
	for _, hi := range healItems {
		it, ok := c.S.FindItem(hi.name, gsi.Inventory)
		if ok && it.CanCast && it.Cooldown == 0 && it.Charges >= hi.minCharges {
			return hi.name
		}
	}
	return ""
}

func spendableGold(c *Ctx) int {
	h := c.S.Hero
	if isCore(c.Settings.Role) && c.Clock >= c.T.BuybackFrom && h.BuybackCooldown == 0 {
		return c.S.Player.Gold - h.BuybackCost
	}
	return c.S.Player.Gold
}

func (c *Ctx) nextBuildItem() (string, int, bool) {
	name, cost, ok := c.nextItemCost()
	return name, max(cost-c.S.Player.Gold, 0), ok
}

// nextItemCost is the next build item and what it still costs, before the gold the player holds.
func (c *Ctx) nextItemCost() (string, int, bool) {
	items, b := c.items(), c.build()
	if items == nil || b == nil || c.Clock < 0 {
		return "", 0, false
	}
	held := heldNames(c.S)
	next, ok := b.Next(held, items, c.Clock)
	if !ok {
		return "", 0, false
	}
	return next.DName, dotadata.RemainingCost(next.Name, held, items), true
}

func (c *Ctx) nextItemGoal() (ItemGoal, bool) {
	items := c.items()
	if items == nil {
		return ItemGoal{}, false
	}
	owned := dotadata.OwnedClosure(heldNames(c.S), items)
	for _, g := range c.Targets.Items {
		if !owned[g.Item] {
			return g, true
		}
	}
	return ItemGoal{}, false
}

func (c *Ctx) itemGoalGold() int {
	g, ok := c.nextItemGoal()
	if !ok {
		return 0
	}
	return max(dotadata.RemainingCost(g.Item, heldNames(c.S), c.items())-c.S.Player.Gold, 0)
}

// seeIdle tracks where the hero was and what they had, for idleFor.
func (m *match) seeIdle(s *gsi.State, clock int) {
	a := &m.idle
	h, p := s.Hero, s.Player
	if !h.Alive || clock < idleFromMS || (h.XPos == 0 && h.YPos == 0) || nearFountain(s) {
		a.set = false
		return
	}
	progress := p.LastHits + p.Denies + p.Kills + p.Assists
	moved := math.Hypot(float64(h.XPos-a.x), float64(h.YPos-a.y)) > idleMoved
	if !a.set || moved || progress != a.progress {
		*a = anchor{set: true, x: h.XPos, y: h.YPos, clock: clock, progress: progress}
	}
}

// seeRuleEvents records when each event rules can wait on last happened.
func (m *match) seeRuleEvents(s, prev *gsi.State, clock int) {
	if prev == nil {
		return
	}
	switch {
	case prev.Map.ClockTime < 0 && clock >= 0:
		m.eventAt["horn"] = clock
	}
	if died(prev, s) {
		m.eventAt["died"] = clock
	}
	if !prev.Hero.Alive && s.Hero.Alive {
		m.eventAt["respawned"] = clock
	}
	if s.Hero.Level > prev.Hero.Level {
		m.eventAt["level_up"] = clock
	}
	if s.Player.Kills > prev.Player.Kills {
		m.eventAt["kill"] = clock
	}
}

func (m *match) idleFor(clock int) int {
	if !m.idle.set {
		return 0
	}
	return clock - m.idle.clock
}

// sinceRoshan is the seconds since Roshan was last seen dying, or a number no rule reaches.
func (m *match) sinceRoshan(clock int) int {
	if !m.roshanKnown {
		return neverSeconds
	}
	return clock - m.roshanDeadAt
}

func (m *match) aegisLeft(clock int) int {
	if !m.aegisKnown || m.aegisUsed || m.aegisExpires <= clock {
		return 0
	}
	return m.aegisExpires - clock
}

// reincarnated is the Aegis bringing the player back: their HP hits 0 as it leaves the inventory.
func reincarnated(prev, s *gsi.State) bool {
	_, had := prev.FindItem("aegis", gsi.Inventory, gsi.Backpack)
	_, has := s.FindItem("aegis", gsi.Inventory, gsi.Backpack)
	return prev.Hero.Alive && !s.Hero.Alive && had && !has
}

// died is a real death: Dota doesn't count coming back with the Aegis as one.
func died(prev, s *gsi.State) bool {
	return prev.Hero.Alive && !s.Hero.Alive && !reincarnated(prev, s)
}

// aegisWhose is "mine", "team", "enemy", or empty when the pickup didn't say.
func aegisWhose(c *Ctx) string {
	m := c.m
	_, has := c.S.FindItem("aegis", gsi.Inventory, gsi.Backpack)
	switch {
	case m.aegisMine || has:
		return "mine"
	case m.aegisTeam == "":
		return ""
	case m.aegisTeam == m.team:
		return "team"
	}
	return "enemy"
}

var aegisNames = map[string][2]string{
	"mine": {"Your Aegis", "Ваш Аегис"}, "team": {"Your team's Aegis", "Аегис вашей команды"},
	"enemy": {"The enemy's Aegis", "Аегис врага"}, "": {"The Aegis", "Аегис"},
}

const neverSeconds = 99999

// earnedGold is everything the player has taken in this match, however they got it.
func earnedGold(s *gsi.State) int {
	p := s.Player
	return p.GoldFromCreeps + p.GoldFromHeroes + p.GoldFromIncome + p.GoldFromShared
}

func fightGoldPct(s *gsi.State) int {
	total := earnedGold(s)
	if total <= 0 {
		return 0
	}
	return s.Player.GoldFromHeroes * 100 / total
}

func ownBuildings(c *Ctx) map[string]gsi.Building {
	if c.S.Buildings == nil {
		return nil
	}
	return c.S.Buildings[c.S.Player.TeamName]
}

// lowestTower is the standing building of yours with the least health left.
func lowestTower(c *Ctx) (string, int) {
	name, pct := "", 100
	for key, b := range ownBuildings(c) {
		if b.MaxHealth <= 0 || b.Health <= 0 {
			continue
		}
		if p := b.Health * 100 / b.MaxHealth; p < pct {
			name, pct = key, p
		}
	}
	return name, pct
}

// powerRuneSpots are the river rune spots, from where the player's hero stood when bottling
// runes in recorded games.
var powerRuneSpots = []struct {
	side string
	x, y float64
}{{"top", -1580, 1010}, {"bottom", 1200, -1080}}

var runeSidesRU = map[string]string{"top": "верхней", "bottom": "нижней"}

// nearestPowerRune is the closer river rune spot and how far the hero is from it.
func nearestPowerRune(s *gsi.State) (string, float64) {
	side, best := "", math.Inf(1)
	for _, spot := range powerRuneSpots {
		if d := math.Hypot(float64(s.Hero.XPos)-spot.x, float64(s.Hero.YPos)-spot.y); d < best {
			side, best = spot.side, d
		}
	}
	return side, best
}

// healthOf is a building's health in percent, or 100 when it isn't known.
func healthOf(c *Ctx, key string) int {
	b, ok := ownBuildings(c)[key]
	if !ok || b.MaxHealth <= 0 {
		return 100
	}
	return b.Health * 100 / b.MaxHealth
}

// fallingBuilding is a building of yours that lost health since the last update.
func fallingBuilding(c *Ctx) string {
	if c.Prev == nil || c.Prev.Buildings == nil {
		return ""
	}
	before := c.Prev.Buildings[c.S.Player.TeamName]
	worst, drop := "", 0
	for key, b := range ownBuildings(c) {
		was, ok := before[key]
		if !ok || b.MaxHealth <= 0 {
			continue
		}
		if d := was.Health - b.Health; d > drop {
			worst, drop = key, d
		}
	}
	return worst
}

// dropWindow is how far back a building's health is compared to spot a real push.
const dropWindow = 5

type buildingSample struct {
	clock int
	pct   map[string]int
}

// seeBuildings keeps a few seconds of your buildings' health, to tell a push from a creep wave.
func (m *match) seeBuildings(s *gsi.State, clock int) {
	if s.Buildings == nil {
		return
	}
	own := s.Buildings[s.Player.TeamName]
	if len(own) == 0 {
		return
	}
	pct := make(map[string]int, len(own))
	for key, b := range own {
		if b.MaxHealth > 0 {
			pct[key] = b.Health * 100 / b.MaxHealth
		}
	}
	// A second's last update stands for it, so a building that just fell isn't still dropping.
	if n := len(m.buildings); n > 0 && m.buildings[n-1].clock == clock {
		m.buildings[n-1].pct = pct
		return
	}
	m.buildings = append(m.buildings, buildingSample{clock: clock, pct: pct})
	for len(m.buildings) > 0 && clock-m.buildings[0].clock > dropWindow {
		m.buildings = m.buildings[1:]
	}
}

// buildingDrop is the building of yours that lost the most health over the last few seconds,
// and how many percent it lost.
func (m *match) buildingDrop(only func(key string) bool) (string, int) {
	if len(m.buildings) < 2 {
		return "", 0
	}
	first, last := m.buildings[0].pct, m.buildings[len(m.buildings)-1].pct
	worst, drop := "", 0
	for key, now := range last {
		if only != nil && !only(key) {
			continue
		}
		if was, ok := first[key]; ok && now > 0 && was-now > drop {
			worst, drop = key, was-now
		}
	}
	return worst, drop
}

// inBase is a building whose loss opens up the base: tier 3 and 4 towers, barracks, the ancient.
func inBase(key string) bool {
	return strings.Contains(key, "tower3") || strings.Contains(key, "tower4") || strings.Contains(key, "_rax_") || strings.HasSuffix(key, "_fort")
}

var buildingLanes = map[string]string{"top": "top", "mid": "mid", "bot": "bottom"}

var buildingLanesRU = map[string]string{"top": "на верхней линии", "mid": "на миде", "bot": "на нижней линии"}

// buildingLabelIn names a building in the player's language.
func buildingLabelIn(key, lang string) string {
	if lang != "ru" || key == "" {
		return buildingLabel(key)
	}
	lane := buildingLanesRU[key[strings.LastIndex(key, "_")+1:]]
	switch {
	case strings.HasSuffix(key, "_fort"):
		return "трон"
	case strings.Contains(key, "_rax_"):
		kind := "бараки ближнего боя"
		if strings.Contains(key, "_range") {
			kind = "бараки дальнего боя"
		}
		return strings.TrimSpace(kind + " " + lane)
	case strings.Contains(key, "tower"):
		i := strings.Index(key, "tower")
		return strings.TrimSpace("башня " + key[i+5:i+6] + "-го тира " + lane)
	}
	return buildingLabel(key)
}

// buildingLabel turns a key like dota_goodguys_tower2_mid into words.
func buildingLabel(key string) string {
	switch {
	case key == "":
		return ""
	case strings.HasSuffix(key, "_fort"):
		return "ancient"
	case strings.Contains(key, "_rax_"):
		kind := "melee"
		if strings.Contains(key, "_range") {
			kind = "ranged"
		}
		return kind + " barracks " + buildingLanes[key[strings.LastIndex(key, "_")+1:]]
	case strings.Contains(key, "tower"):
		i := strings.Index(key, "tower")
		tier := key[i+5 : i+6]
		lane := buildingLanes[key[strings.LastIndex(key, "_")+1:]]
		if lane == "" {
			return "tier " + tier + " tower"
		}
		return lane + " tier " + tier + " tower"
	}
	return strings.ReplaceAll(strings.TrimPrefix(strings.TrimPrefix(key, "dota_goodguys_"), "dota_badguys_"), "_", " ")
}
