package coach

import (
	"math"
	"slices"
	"strings"

	"gourdian/internal/game/dota"
	"gourdian/internal/game/gsi"
)

// Field is a game value a custom rule can test or put in its text.
type Field struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Group string `json:"group"`
	Type  string `json:"type"`           // "number", "bool" or "text"
	Unit  string `json:"unit,omitempty"` // "clock", "seconds", "percent" or "gold"
	// Arg is "item" or "ability" when the field needs one picked, such as which item to look for.
	Arg  string `json:"arg,omitempty"`
	Help string `json:"help,omitempty"`

	num  func(c *Ctx, arg string) float64
	flag func(c *Ctx, arg string) bool
	text func(c *Ctx, arg string) string
}

func num(f func(c *Ctx) float64) func(*Ctx, string) float64 {
	return func(c *Ctx, _ string) float64 { return f(c) }
}

func flag(f func(c *Ctx) bool) func(*Ctx, string) bool {
	return func(c *Ctx, _ string) bool { return f(c) }
}

func f64[T ~int](v T) float64 { return float64(v) }

// Fields lists what custom rules can use, grouped for the editor.
var Fields = []Field{
	{ID: "clock", Label: "Game clock", Group: "Match", Type: "number", Unit: "clock", Help: "Minutes and seconds on the in-game clock; negative before the horn.",
		num: num(func(c *Ctx) float64 { return f64(c.Clock) })},
	{ID: "daytime", Label: "It's day", Group: "Match", Type: "bool", flag: flag(func(c *Ctx) bool { return c.S.Map.Daytime })},
	{ID: "score_lead", Label: "Team kill lead", Group: "Match", Type: "number", Help: "Your team's kills minus the enemy's.",
		num: num(func(c *Ctx) float64 {
			lead := c.S.Map.RadiantScore - c.S.Map.DireScore
			if c.S.Player.TeamName == "dire" {
				lead = -lead
			}
			return f64(lead)
		})},
	{ID: "role_name", Label: "Position, in words", Group: "Match", Type: "text",
		Help: "carry, mid, offlane, soft support or hard support.",
		text: func(c *Ctx, _ string) string { return dota.RoleName(c.Settings.Role, c.Settings.Language) }},
	{ID: "role_why", Label: "Why that position", Group: "Match", Type: "text",
		Help: "your pick, your usual on this hero, or the lane you played.",
		text: func(c *Ctx, _ string) string { return c.RoleNote }},
	{ID: "focus", Label: "Focus from your last review", Group: "Match", Type: "text",
		text: func(c *Ctx, _ string) string { return c.Focus }},
	{ID: "roshan_killer", Label: "Who killed Roshan", Group: "Match", Type: "text",
		Help: "\"by your team\", \"by the enemy team\" or empty when Dota doesn't say.",
		text: func(c *Ctx, _ string) string {
			if c.Settings.Language == "ru" {
				return map[bool]string{true: "вашей командой", false: "врагами"}[c.m.roshan.team == c.m.team]
			}
			return strings.TrimSpace(teamSuffix(c.m.roshan.team, c.m.team, "by"))
		}},
	{ID: "roshan_min", Label: "Earliest Roshan respawn", Group: "Match", Type: "number", Unit: "clock",
		num: num(func(c *Ctx) float64 {
			if !c.m.roshan.known {
				return 0
			}
			return f64(c.m.roshan.deadAt + c.T.RoshanRespawnMin)
		})},
	{ID: "roshan_max", Label: "Latest Roshan respawn", Group: "Match", Type: "number", Unit: "clock",
		num: num(func(c *Ctx) float64 {
			if !c.m.roshan.known {
				return 0
			}
			return f64(c.m.roshan.deadAt + c.T.RoshanRespawnMax)
		})},
	{ID: "aegis_holder", Label: "Whose Aegis", Group: "Match", Type: "text",
		Help: "Yours, your team's or the enemy's.",
		text: func(c *Ctx, _ string) string {
			names := aegisNames[aegisWhose(c)]
			if c.Settings.Language == "ru" {
				return names[1]
			}
			return names[0]
		}},
	{ID: "aegis_expires", Label: "Time your Aegis expires", Group: "Match", Type: "number", Unit: "clock",
		Help: "Only meaningful while aegis_left is above zero, which is only for your own Aegis.",
		num:  num(func(c *Ctx) float64 { return f64(c.m.aegis.expires) })},
	{ID: "tower_low", Label: "Weakest tower of yours", Group: "Match", Type: "number", Unit: "percent",
		Help: "The health left on your lowest standing building, 100 when everything is full.",
		num:  num(func(c *Ctx) float64 { _, pct := lowestTower(c); return f64(pct) })},
	{ID: "attacked_hp", Label: "Health of the building under attack", Group: "Match", Type: "number", Unit: "percent",
		Help: "The building of yours taking damage right now; 100 when none is.",
		num:  num(func(c *Ctx) float64 { return f64(healthOf(c, fallingBuilding(c))) })},
	{ID: "tower_falling", Label: "A building of yours is taking damage", Group: "Match", Type: "bool",
		flag: flag(func(c *Ctx) bool { return fallingBuilding(c) != "" })},
	{ID: "tower_name", Label: "Which building of yours", Group: "Match", Type: "text",
		Help: "The one taking damage, or your weakest one.",
		text: func(c *Ctx, _ string) string {
			if key := fallingBuilding(c); key != "" {
				return buildingLabelIn(key, c.Settings.Language)
			}
			key, _ := lowestTower(c)
			return buildingLabelIn(key, c.Settings.Language)
		}},
	{ID: "tower_drop", Label: "Health a building of yours lost in 5 seconds", Group: "Match", Type: "number", Unit: "percent",
		Help: "The biggest loss among your buildings over the last 5 seconds: a creep wave takes a few percent, a push by heroes much more.",
		num:  num(func(c *Ctx) float64 { _, d := c.m.buildingDrop(nil); return f64(d) })},
	{ID: "dropping_hp", Label: "Health of the building dropping fastest", Group: "Match", Type: "number", Unit: "percent",
		num: num(func(c *Ctx) float64 { key, _ := c.m.buildingDrop(nil); return f64(healthOf(c, key)) })},
	{ID: "base_building", Label: "Which base building is under attack", Group: "Match", Type: "text",
		text: func(c *Ctx, _ string) string {
			key, _ := c.m.buildingDrop(inBase)
			return buildingLabelIn(key, c.Settings.Language)
		}},
	{ID: "dropping_building", Label: "Which building is dropping", Group: "Match", Type: "text",
		text: func(c *Ctx, _ string) string {
			key, _ := c.m.buildingDrop(nil)
			return buildingLabelIn(key, c.Settings.Language)
		}},
	{ID: "base_under_attack", Label: "Your base is under attack", Group: "Match", Type: "bool",
		Help: "A tier 3 or 4 tower, a barracks or the ancient of yours is losing health.",
		flag: flag(func(c *Ctx) bool { _, d := c.m.buildingDrop(inBase); return d > 0 })},
	{ID: "glyph_ready", Label: "Your Glyph is ready", Group: "Match", Type: "bool",
		Help: "Your team's Glyph comes back 5 minutes after use, or as soon as a tower or barracks of yours falls.",
		flag: flag(func(c *Ctx) bool { return c.m.glyphReady(c.Clock, c.T.GlyphCooldown) })},
	{ID: "melee_rax_lost", Label: "Melee barracks of yours destroyed", Group: "Match", Type: "number",
		num: num(func(c *Ctx) float64 {
			n := 0
			for key, b := range ownBuildings(c) {
				if strings.Contains(key, "_rax_melee") && b.MaxHealth > 0 && b.Health <= 0 {
					n++
				}
			}
			return f64(n)
		})},
	{ID: "towers_lost", Label: "Buildings of yours destroyed", Group: "Match", Type: "number",
		num: num(func(c *Ctx) float64 {
			n := 0
			for _, b := range ownBuildings(c) {
				if b.MaxHealth > 0 && b.Health <= 0 {
					n++
				}
			}
			return f64(n)
		})},
	{ID: "power_rune_distance", Label: "Distance to the nearest power rune spot", Group: "Map", Type: "number",
		Help: "In map units: a hero crosses about 3000 in 10 seconds.",
		num:  num(func(c *Ctx) float64 { _, d := nearestPowerRune(c.S); return d })},
	{ID: "power_rune_side", Label: "Nearest power rune spot", Group: "Map", Type: "text",
		Help: "top or bottom river.",
		text: func(c *Ctx, _ string) string {
			side, _ := nearestPowerRune(c.S)
			if c.Settings.Language == "ru" {
				return runeSidesRU[side]
			}
			return side
		}},
	{ID: "ward_cooldown", Label: "Time until observer wards restock", Group: "Items", Type: "number", Unit: "seconds",
		Help: "0 while the stock is full. Dota doesn't say how many wards are in stock.",
		num:  num(func(c *Ctx) float64 { return f64(c.S.Map.WardPurchaseCooldown) })},
	{ID: "wards_in_shop", Label: "Observer ward stock is full", Group: "Items", Type: "bool",
		Help: "No more wards restock until someone buys one.",
		flag: flag(func(c *Ctx) bool { return c.S.Map.WardPurchaseCooldown <= 0 })},
	{ID: "role", Label: "Position", Group: "Match", Type: "text", Help: "carry, mid, offlane, soft_support or hard_support.",
		text: func(c *Ctx, _ string) string { return c.Settings.Role }},

	{ID: "level", Label: "Hero level", Group: "Hero", Type: "number", num: num(func(c *Ctx) float64 { return f64(c.S.Hero.Level) })},
	{ID: "alive", Label: "Hero is alive", Group: "Hero", Type: "bool", flag: flag(func(c *Ctx) bool { return c.S.Hero.Alive })},
	{ID: "respawn", Label: "Respawn time", Group: "Hero", Type: "number", Unit: "seconds", num: num(func(c *Ctx) float64 { return f64(c.S.Hero.RespawnSeconds) })},
	{ID: "hp", Label: "Health", Group: "Hero", Type: "number", num: num(func(c *Ctx) float64 { return f64(c.S.Hero.Health) })},
	{ID: "hp_pct", Label: "Health %", Group: "Hero", Type: "number", Unit: "percent", num: num(func(c *Ctx) float64 { return f64(c.S.Hero.HealthPercent) })},
	{ID: "mana_pct", Label: "Mana %", Group: "Hero", Type: "number", Unit: "percent", num: num(func(c *Ctx) float64 { return f64(c.S.Hero.ManaPercent) })},
	{ID: "stunned", Label: "Stunned", Group: "Hero", Type: "bool", flag: flag(func(c *Ctx) bool { return c.S.Hero.Stunned })},
	{ID: "silenced", Label: "Silenced", Group: "Hero", Type: "bool", flag: flag(func(c *Ctx) bool { return c.S.Hero.Silenced })},
	{ID: "smoked", Label: "Smoked", Group: "Hero", Type: "bool", flag: flag(func(c *Ctx) bool { return c.S.Hero.Smoked })},
	{ID: "magic_immune", Label: "Magic immune", Group: "Hero", Type: "bool", flag: flag(func(c *Ctx) bool { return c.S.Hero.MagicImmune })},
	{ID: "has_scepter", Label: "Has Aghanim's Scepter", Group: "Hero", Type: "bool", flag: flag(func(c *Ctx) bool { return c.S.Hero.AghanimsScepter })},
	{ID: "has_shard", Label: "Has Aghanim's Shard", Group: "Hero", Type: "bool", flag: flag(func(c *Ctx) bool { return c.S.Hero.AghanimsShard })},
	{ID: "near_fountain", Label: "In base", Group: "Hero", Type: "bool", flag: flag(func(c *Ctx) bool { return nearFountain(c.S) })},
	{ID: "deaths_match", Label: "Deaths this match", Group: "Hero", Type: "number", num: num(func(c *Ctx) float64 { return f64(len(c.m.deaths)) })},
	{ID: "since_death", Label: "Time since last death", Group: "Hero", Type: "number", Unit: "seconds", Help: "Very large if you haven't died.",
		num: num(func(c *Ctx) float64 {
			if n := len(c.m.deaths); n > 0 {
				return f64(c.Clock - c.m.deaths[n-1])
			}
			return math.MaxInt32
		})},

	{ID: "gold", Label: "Gold", Group: "Economy", Type: "number", Unit: "gold", num: num(func(c *Ctx) float64 { return f64(c.S.Player.Gold) })},
	{ID: "gold_reliable", Label: "Reliable gold", Group: "Economy", Type: "number", Unit: "gold", num: num(func(c *Ctx) float64 { return f64(c.S.Player.GoldReliable) })},
	{ID: "gpm", Label: "GPM", Group: "Economy", Type: "number", num: num(func(c *Ctx) float64 { return f64(c.S.Player.GPM) })},
	{ID: "xpm", Label: "XPM", Group: "Economy", Type: "number", num: num(func(c *Ctx) float64 { return f64(c.S.Player.XPM) })},
	{ID: "last_hits", Label: "Last hits", Group: "Economy", Type: "number", num: num(func(c *Ctx) float64 { return f64(c.S.Player.LastHits) })},
	{ID: "denies", Label: "Denies", Group: "Economy", Type: "number", num: num(func(c *Ctx) float64 { return f64(c.S.Player.Denies) })},
	{ID: "pace_diff", Label: "Last hits vs pace", Group: "Economy", Type: "number", Help: "Your last hits minus the pace for your position; negative when behind.",
		num: num(func(c *Ctx) float64 {
			exp, ok := expectedLastHits(c.Targets.LastHits, c.Clock)
			if !ok {
				return 0
			}
			return f64(c.S.Player.LastHits - exp)
		})},
	{ID: "buyback_cost", Label: "Buyback cost", Group: "Economy", Type: "number", Unit: "gold", num: num(func(c *Ctx) float64 { return f64(c.S.Hero.BuybackCost) })},
	{ID: "buyback_cooldown", Label: "Buyback cooldown", Group: "Economy", Type: "number", Unit: "seconds", num: num(func(c *Ctx) float64 { return f64(c.S.Hero.BuybackCooldown) })},
	{ID: "can_buyback", Label: "Can buy back", Group: "Economy", Type: "bool",
		flag: flag(func(c *Ctx) bool {
			h := c.S.Hero
			return h.BuybackCost > 0 && h.BuybackCooldown == 0 && c.S.Player.Gold >= h.BuybackCost
		})},

	{ID: "kills", Label: "Kills", Group: "Score", Type: "number", num: num(func(c *Ctx) float64 { return f64(c.S.Player.Kills) })},
	{ID: "deaths", Label: "Deaths", Group: "Score", Type: "number", num: num(func(c *Ctx) float64 { return f64(c.S.Player.Deaths) })},
	{ID: "assists", Label: "Assists", Group: "Score", Type: "number", num: num(func(c *Ctx) float64 { return f64(c.S.Player.Assists) })},
	{ID: "kill_streak", Label: "Kill streak", Group: "Score", Type: "number", num: num(func(c *Ctx) float64 { return f64(c.S.Player.KillStreak) })},

	{ID: "has_item", Label: "Has item", Group: "Items", Type: "bool", Arg: "item", Help: "In the inventory, backpack or stash.",
		flag: func(c *Ctx, arg string) bool {
			_, ok := c.S.FindItem(arg, gsi.Inventory, gsi.Backpack, gsi.Stash)
			return ok || arg == "tpscroll" && !c.S.Items["teleport0"].Empty()
		}},
	{ID: "item_in_stash", Label: "Item is in the stash", Group: "Items", Type: "bool", Arg: "item",
		flag: func(c *Ctx, arg string) bool { _, ok := c.S.FindItem(arg, gsi.Stash); return ok }},
	{ID: "item_in_backpack", Label: "Item is in the backpack", Group: "Items", Type: "bool", Arg: "item",
		flag: func(c *Ctx, arg string) bool { _, ok := c.S.FindItem(arg, gsi.Backpack); return ok }},
	{ID: "item_ready", Label: "Item is ready to use", Group: "Items", Type: "bool", Arg: "item", Help: "In the inventory and off cooldown.",
		flag: func(c *Ctx, arg string) bool {
			it, ok := c.S.FindItem(arg, gsi.Inventory)
			return ok && it.CanCast && it.Cooldown == 0
		}},
	{ID: "item_charges", Label: "Item charges", Group: "Items", Type: "number", Arg: "item",
		num: func(c *Ctx, arg string) float64 {
			it, _ := c.S.FindItem(arg, gsi.Inventory, gsi.Backpack, gsi.Stash)
			return f64(it.Charges)
		}},
	{ID: "stash_items", Label: "Items in the stash", Group: "Items", Type: "number", num: num(func(c *Ctx) float64 { return f64(len(c.S.ItemsIn(gsi.Stash))) })},
	{ID: "stash_contents", Label: "What is in the stash", Group: "Items", Type: "text",
		Help: "The stash items by name, in order. A reminder keyed on this comes back when something new lands there, not while the courier fetches what is already known about.",
		text: func(c *Ctx, _ string) string {
			names := make([]string, 0, 6)
			for _, it := range c.S.ItemsIn(gsi.Stash) {
				names = append(names, it.Short())
			}
			slices.Sort(names)
			return strings.Join(names, ",")
		}},
	{ID: "empty_slots", Label: "Empty inventory slots", Group: "Items", Type: "number",
		num: num(func(c *Ctx) float64 { return f64(6 - len(c.S.ItemsIn(gsi.Inventory))) })},
	{ID: "neutral_empty", Label: "Neutral slot is empty", Group: "Items", Type: "bool", flag: flag(func(c *Ctx) bool { return c.S.Items["neutral0"].Empty() })},
	{ID: "has_tp", Label: "Has a TP scroll", Group: "Items", Type: "bool", Help: "A scroll or travel boots, anywhere including the stash.",
		flag: flag(func(c *Ctx) bool {
			if !c.S.Items["teleport0"].Empty() {
				return true
			}
			for _, name := range []string{"tpscroll", "travel_boots", "travel_boots_2"} {
				if _, found := c.S.FindItem(name, gsi.Inventory, gsi.Backpack, gsi.Stash); found {
					return true
				}
			}
			return false
		})},
	{ID: "tp_cost", Label: "TP scroll cost", Group: "Items", Type: "number", Unit: "gold",
		num: num(func(c *Ctx) float64 {
			if info, ok := c.items()["tpscroll"]; ok && info.Cost > 0 {
				return f64(info.Cost)
			}
			return 100
		})},
	{ID: "shard_cost", Label: "Aghanim's Shard cost", Group: "Items", Type: "number", Unit: "gold",
		num: num(shardCost)},
	{ID: "shard_on_sale", Label: "Aghanim's Shard is on sale", Group: "Items", Type: "bool",
		Help: "From 15:00, and only while you don't have one; a Tormentor's Shard counts.",
		flag: flag(func(c *Ctx) bool { return c.Clock >= c.T.ShardFrom && !c.S.Hero.AghanimsShard })},
	{ID: "shard_in_build", Label: "Aghanim's Shard is in the popular build", Group: "Items", Type: "bool",
		Help: "Whether the build for your hero and position buys a Shard. Many heroes never do.",
		flag: flag(shardInBuild)},
	{ID: "shard_gold", Label: "Gold still needed for Aghanim's Shard", Group: "Items", Type: "number", Unit: "gold",
		Help: "0 once you can afford it.",
		num: num(func(c *Ctx) float64 {
			return math.Max(shardCost(c)-f64(c.S.Player.Gold), 0)
		})},
	{ID: "backpack_active", Label: "An active item is in the backpack", Group: "Items", Type: "bool",
		Help: "Wand, BKB, blink and the like do nothing from the backpack.",
		flag: flag(func(c *Ctx) bool { return backpackActive(c.S) != "" })},
	{ID: "backpack_item", Label: "Active item in the backpack", Group: "Items", Type: "text",
		text: func(c *Ctx, _ string) string { return c.itemName(backpackActive(c.S)) }},
	{ID: "holding_wards", Label: "Carrying observer wards", Group: "Items", Type: "bool",
		flag: flag(func(c *Ctx) bool {
			_, obs := c.S.FindItem("ward_observer", gsi.Inventory, gsi.Backpack)
			_, disp := c.S.FindItem("ward_dispenser", gsi.Inventory, gsi.Backpack)
			return obs || disp
		})},

	{ID: "heal_item", Label: "Healing item ready", Group: "Items", Type: "text",
		Help: "The name of a wand, fire, cheese or mek you can use right now, empty when there is none.",
		text: func(c *Ctx, _ string) string { return c.itemName(readyHeal(c)) }},
	{ID: "heal_advice", Label: "What to do at low HP", Group: "Items", Type: "text",
		Help: "Use your healing item and back off, or just back off when there is none.",
		text: func(c *Ctx, _ string) string {
			item := c.itemName(readyHeal(c))
			switch {
			case c.Settings.Language == "ru" && item != "":
				return "Используйте " + item + " и отойдите"
			case c.Settings.Language == "ru":
				return "Отойдите"
			case item != "":
				return "Use " + item + " and back off"
			}
			return "Back off"
		}},
	{ID: "has_heal_item", Label: "A healing item is ready", Group: "Items", Type: "bool",
		flag: flag(func(c *Ctx) bool { return readyHeal(c) != "" })},
	{ID: "next_item", Label: "Next item in the build", Group: "Items", Type: "text",
		text: func(c *Ctx, _ string) string { name, _, _ := c.nextBuildItem(); return name }},
	{ID: "next_item_gold", Label: "Gold still needed for the next item", Group: "Items", Type: "number", Unit: "gold",
		num: num(func(c *Ctx) float64 { _, left, _ := c.nextBuildItem(); return f64(left) })},
	{ID: "next_item_ready", Label: "The next item is affordable", Group: "Items", Type: "bool",
		flag: flag(func(c *Ctx) bool { _, left, ok := c.nextBuildItem(); return ok && left <= 0 })},
	{ID: "item_goal", Label: "Next core item goal", Group: "Items", Type: "text",
		text: func(c *Ctx, _ string) string {
			g, ok := c.nextItemGoal()
			if !ok {
				return ""
			}
			return g.Name
		}},
	{ID: "item_goal_due", Label: "Time the core item is due", Group: "Items", Type: "number", Unit: "clock",
		num: num(func(c *Ctx) float64 { g, _ := c.nextItemGoal(); return f64(g.By) })},
	{ID: "build_switched", Label: "You built something else instead", Group: "Items", Type: "bool",
		Help: "True when a different big item was finished in place of the core item goal.",
		flag: flag(func(c *Ctx) bool {
			g, ok := c.nextItemGoal()
			return ok && c.switchedFrom(g, c.items())
		})},
	{ID: "item_goal_in", Label: "Time until the core item goal", Group: "Items", Type: "number", Unit: "seconds",
		Help: "Negative once the goal has passed. Very large when there is no goal.",
		num: num(func(c *Ctx) float64 {
			g, ok := c.nextItemGoal()
			if !ok || g.By == 0 {
				return neverSeconds
			}
			return f64(g.By - c.Clock)
		})},
	{ID: "item_goal_gold", Label: "Gold still needed for the core item", Group: "Items", Type: "number", Unit: "gold",
		num: num(func(c *Ctx) float64 { return f64(c.itemGoalGold()) })},
	{ID: "neutral_tier", Label: "Neutral item tier unlocked", Group: "Items", Type: "number",
		num: num(func(c *Ctx) float64 {
			n := 0
			for _, at := range c.T.NeutralTiers {
				if c.Clock >= at {
					n++
				}
			}
			return f64(n)
		})},
	{ID: "fight_gold_pct", Label: "Share of your gold from kills", Group: "Economy", Type: "number", Unit: "percent",
		Help: "Gold from hero kills against everything you have earned this match.",
		num:  num(func(c *Ctx) float64 { return f64(fightGoldPct(c.S)) })},
	{ID: "earned_gold", Label: "Gold earned this match", Group: "Economy", Type: "number", Unit: "gold",
		num: num(func(c *Ctx) float64 { return f64(earnedGold(c.S)) })},
	{ID: "gold_before_death", Label: "Unreliable gold you died with", Group: "Economy", Type: "number", Unit: "gold",
		Help: "Read from the last moment you had health, since Dota takes part of it as you die.",
		num:  num(func(c *Ctx) float64 { return f64(c.m.unreliable) })},
	{ID: "gold_unreliable", Label: "Unreliable gold", Group: "Economy", Type: "number", Unit: "gold",
		Help: "The gold you lose part of when you die: everything but reliable gold from kills, Roshan and objectives.",
		num:  num(func(c *Ctx) float64 { return f64(c.S.Player.Gold - c.S.Player.GoldReliable) })},
	{ID: "gold_spendable", Label: "Gold you can spend", Group: "Economy", Type: "number", Unit: "gold",
		Help: "Your gold, minus buyback money once buyback matters for your position.",
		num:  num(func(c *Ctx) float64 { return f64(spendableGold(c)) })},
	{ID: "gold_beyond_next", Label: "Gold beyond what the next item needs", Group: "Economy", Type: "number", Unit: "gold",
		Help: "Gold you can spend minus what your next build item still costs, so saving up for it doesn't count as unspent.",
		num: num(func(c *Ctx) float64 {
			if _, cost, ok := c.nextItemCost(); ok {
				return f64(spendableGold(c) - cost)
			}
			return f64(spendableGold(c))
		})},
	{ID: "idle_income", Label: "Income sitting unspent", Group: "Economy", Type: "number", Unit: "seconds",
		Help: "How long it took to earn the gold you are carrying, at your current GPM.",
		num: num(func(c *Ctx) float64 {
			gpm := c.S.Player.GPM
			if gpm <= 0 {
				return 0
			}
			return f64(spendableGold(c) * 60 / gpm)
		})},
	{ID: "idle_seconds", Label: "Seconds without farm or movement", Group: "Map", Type: "number", Unit: "seconds",
		Help: "Counts once you have not moved 500 units and gained no last hit, deny, kill or assist.",
		num:  num(func(c *Ctx) float64 { return f64(c.m.idleFor(c.Clock)) })},
	{ID: "lh_usual", Label: "Your usual last hits by now", Group: "Economy", Type: "number",
		Help: "Your own median on this hero and position, 0 until you have a few games.",
		num: num(func(c *Ctx) float64 {
			v, _ := expectedLastHits(c.Targets.Usual, c.Clock)
			return f64(v)
		})},
	{ID: "lh_target", Label: "Last-hit target for now", Group: "Economy", Type: "number",
		Help: "The last hits your position and hero should have at this point in the match.",
		num:  num(func(c *Ctx) float64 { v, _ := expectedLastHits(c.Targets.LastHits, c.Clock); return f64(v) })},
	{ID: "roshan_dead_for", Label: "Seconds since Roshan died", Group: "Match", Type: "number", Unit: "seconds",
		Help: "Very large until Roshan is seen dying.",
		num:  num(func(c *Ctx) float64 { return f64(c.m.sinceRoshan(c.Clock)) })},
	{ID: "aegis_left", Label: "Seconds of your Aegis left", Group: "Match", Type: "number", Unit: "seconds",
		Help: "Only your own Aegis, which the trainer sees leave your inventory. Nothing in the game says when anyone else's is spent, so it stays 0 for theirs.",
		num:  num(func(c *Ctx) float64 { return f64(c.m.aegisLeft(c.Clock)) })},
	{ID: "skill_points", Label: "Unspent skill points", Group: "Abilities", Type: "number",
		Help: "Counted against the fewest the hero has had this match, so innate and auto-levelled abilities don't show up as unspent.",
		num:  num(func(c *Ctx) float64 { return f64(c.m.skills.spare()) })},
	{ID: "talent_points", Label: "Unspent talents", Group: "Abilities", Type: "number",
		Help: "Talents have their own points since 7.40, so an unspent one doesn't show up as a skill point.",
		num:  num(func(c *Ctx) float64 { return f64(talentSpare(c.S)) })},
	{ID: "talent_level", Label: "Level of the waiting talent", Group: "Abilities", Type: "number",
		Help: "The level that handed out the talent you haven't taken, or 0 when none is waiting.",
		num: num(func(c *Ctx) float64 {
			return f64(dota.TalentDueAt(c.S.Hero.TalentsTaken(), c.S.Hero.Level))
		})},
	{ID: "next_skill", Label: "Ability pros level next", Group: "Abilities", Type: "text",
		Help: "From OpenDota's recent pro games on this hero and position, worked out from your own ability levels. Empty while it loads.",
		text: func(c *Ctx, _ string) string { return c.nextSkillName() }},
	{ID: "skill_tip", Label: "What to level", Group: "Abilities", Type: "text",
		Help: "Names the ability pros level next, or just says to level up when there's no pro order for the hero.",
		text: func(c *Ctx, _ string) string {
			name := c.nextSkillName()
			switch {
			case c.Settings.Language == "ru" && name != "":
				return "Про-игроки сейчас качают " + name
			case c.Settings.Language == "ru":
				return "Прокачайте способность"
			case name != "":
				return "Pros level " + name + " now"
			}
			return "Level up an ability"
		}},
	{ID: "skill_source", Label: "Where the skill order comes from", Group: "Abilities", Type: "text",
		Help: "The position and number of pro games behind the skill order.",
		text: func(c *Ctx, _ string) string {
			if c.nextSkillName() == "" {
				return ""
			}
			return skillSource(c.skillBuild(), c.Settings.Language)
		}},
	{ID: "ability_ready", Label: "Ability is ready", Group: "Abilities", Type: "bool", Arg: "ability",
		flag: func(c *Ctx, arg string) bool {
			for _, a := range c.S.Abilities {
				if a.Name == arg {
					return a.Level > 0 && a.CanCast && a.Cooldown == 0
				}
			}
			return false
		}},
	{ID: "ultimate_ready", Label: "Ultimate is ready", Group: "Abilities", Type: "bool",
		flag: flag(func(c *Ctx) bool {
			for _, a := range c.S.Abilities {
				if a.Ultimate {
					return a.Level > 0 && a.CanCast && a.Cooldown == 0
				}
			}
			return false
		})},
	{ID: "ultimate_cooldown", Label: "Ultimate cooldown", Group: "Abilities", Type: "number", Unit: "seconds",
		num: num(func(c *Ctx) float64 {
			for _, a := range c.S.Abilities {
				if a.Ultimate {
					return f64(a.Cooldown)
				}
			}
			return 0
		})},
}

var fieldIndex = func() map[string]*Field {
	m := map[string]*Field{}
	for i := range Fields {
		m[Fields[i].ID] = &Fields[i]
	}
	return m
}()

// Event is something a custom rule can react to the moment it happens.
type Event struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Arg   string `json:"arg,omitempty"`
}

var Events = []Event{
	{ID: "horn", Label: "The horn sounds (0:00)"},
	{ID: "died", Label: "You die"},
	{ID: "respawned", Label: "You respawn"},
	{ID: "level_up", Label: "You level up"},
	{ID: "kill", Label: "You get a kill"},
	{ID: "item_bought", Label: "You get an item", Arg: "item"},
	{ID: "roshan_killed", Label: "Roshan is killed"},
	{ID: "aegis_picked_up", Label: "The Aegis is picked up"},
}

// happened reports whether an event occurred in this update.
func (c *Ctx) happened(event, arg string) bool {
	s, p := c.S, c.Prev
	if p == nil {
		return false
	}
	switch event {
	case "horn":
		return p.Map.ClockTime < 0 && c.Clock >= 0
	case "died":
		return died(p, s)
	case "respawned":
		return !p.Hero.Alive && s.Hero.Alive
	case "level_up":
		return s.Hero.Level > p.Hero.Level
	case "kill":
		return s.Player.Kills > p.Player.Kills
	case "item_bought":
		before := map[string]int{}
		for _, n := range heldNames(p) {
			before[n]++
		}
		for _, n := range heldNames(s) {
			if before[n] > 0 {
				before[n]--
			} else if arg == "" || n == arg {
				return true
			}
		}
	case "roshan_killed", "aegis_picked_up":
		for _, ev := range c.m.newEvents {
			if ev == event {
				return true
			}
		}
	}
	return false
}
