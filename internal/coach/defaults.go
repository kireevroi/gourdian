package coach

import "gourdian/internal/dota"

// laningEnds is 10:00, when lanes break up and timed reminders give way to ones about where you are.
const laningEnds = 600

// DefaultSpec is the shipped version of one built-in rule, in English.
func DefaultSpec(id string) (RuleSpec, bool) {
	for _, s := range DefaultSpecs("en") {
		if s.ID == id {
			return s, true
		}
	}
	return RuleSpec{}, false
}

// DefaultSpecs are the built-in rules, written the same way the player writes their own so
// every part of them can be edited. The IDs match the older Go rules, keeping habit counts.
func DefaultSpecs(lang string) []RuleSpec {
	t := dota.DefaultTimings()
	specs := []RuleSpec{
		{
			ID: "role_check", Name: "Say which position you're coached as", Category: "focus",
			When: Trigger{Type: WhenState}, Once: true,
			If: []Cond{
				{Field: "clock", Op: "lt"},
				{Field: "role_name", Op: "is_not", Text: ""},
			},
			Then: AlertSpec{Text: "Coaching you as {role_name} ({role_why}). Ctrl+Shift+1–5 in game for another position",
				Speech: "Coaching you as {role_name}. For another position, press control shift and its number.", Severity: "info"},
		},
		{
			ID: "focus", Name: "Read your focus at the horn", Category: "focus",
			When: Trigger{Type: WhenSchedule, First: 0},
			If:   []Cond{{Field: "focus", Op: "is_not", Text: ""}},
			Then: AlertSpec{Text: "Focus this game: {focus}", Speech: "Focus this game. {focus}", Severity: "info"},
		},
		{
			ID: "roshan", Name: "Roshan is killed", Category: "timing",
			When: Trigger{Type: WhenEvent, Event: "roshan_killed"},
			Then: AlertSpec{Text: "Roshan killed {roshan_killer} at {clock}. Respawns between {roshan_min} and {roshan_max}",
				Speech: "Roshan killed", Severity: "info"},
		},
		{
			ID: "roshan_window", Name: "Roshan may have respawned", Category: "timing",
			When: Trigger{Type: WhenAfter, Event: "roshan_killed", After: t.RoshanRespawnMin},
			Then: AlertSpec{Text: "Roshan may have respawned (certain by {roshan_max})", Speech: "Roshan may be up", Severity: "info"},
		},
		{
			ID: "roshan_up", Name: "Roshan is certainly up", Category: "timing",
			When: Trigger{Type: WhenAfter, Event: "roshan_killed", After: t.RoshanRespawnMax},
			Then: AlertSpec{Text: "Roshan is definitely up", Speech: "Roshan is up", Severity: "warn"},
		},
		{
			ID: "aegis", Name: "Aegis is about to expire", Category: "timing",
			When: Trigger{Type: WhenAfter, Event: "aegis_picked_up", After: t.AegisDuration - 30},
			If:   []Cond{{Field: "aegis_left", Op: "gt"}},
			Then: AlertSpec{Text: "{aegis_holder} expires at {aegis_expires}{aegis_note}", Speech: "{aegis_holder} expires in 30 seconds", Severity: "warn"},
		},
		{
			ID: "death", Name: "Death recap", Category: "survival",
			When: Trigger{Type: WhenEvent, Event: "died"},
			Then: AlertSpec{Text: "Died (death #{deaths_match}). Respawn in {respawn}s, {gold} gold to spend",
				Speech: "Respawn in {respawn} seconds.", Severity: "warn", Mistake: true, Habit: "Deaths",
				Advice: "You die a lot. Check the minimap every few seconds and don't walk past the river without vision or teammates."},
		},
		{
			ID: "death_gold", Name: "Died holding gold", Category: "economy",
			When: Trigger{Type: WhenEvent, Event: "died"},
			If:   []Cond{{Field: "gold_before_death", Op: "ge", Num: 900}},
			Then: AlertSpec{Text: "You died holding {gold_before_death} unreliable gold, and part of it is gone. Spend before fights",
				Speech: "You died with unspent gold. Spend before fights.", Severity: "warn", Mistake: true,
				Habit:  "Died with unspent gold",
				Advice: "Dying takes part of your unreliable gold. Buy components before you walk into a fight."},
		},
		{
			ID: "death_buyback", Name: "No buyback gold when you die", Category: "survival", Roles: cores,
			When: Trigger{Type: WhenEvent, Event: "died"},
			If: []Cond{
				{Field: "clock", Op: "ge", Num: float64(t.BuybackFrom)},
				{Field: "buyback_cooldown", Op: "eq"},
				{Field: "can_buyback", Op: "false"},
			},
			Then: AlertSpec{Text: "No buyback gold: {gold} of {buyback_cost}", Speech: "No buyback gold", Severity: "warn"},
		},
		{
			ID: "item_timing", Name: "Core item goal coming up", Category: "economy", Roles: cores,
			When: Trigger{Type: WhenState},
			If: []Cond{
				{Field: "item_goal", Op: "is_not", Text: ""},
				{Field: "item_goal_in", Op: "between", Num: 0, Num2: 120},
			},
			Then: AlertSpec{Text: "{item_goal} by {item_goal_due}: {item_goal_gold}g to go",
				Speech: "{item_goal} soon. {item_goal_gold} gold to go.", Severity: "info"},
			Cooldown: 300,
		},
		{
			ID: "item_late", Name: "Core item is late", Category: "economy", Roles: cores,
			When: Trigger{Type: WhenState},
			If: []Cond{
				{Field: "item_goal", Op: "is_not", Text: ""},
				{Field: "item_goal_in", Op: "le", Num: -60},
				{Field: "build_switched", Op: "false"},
			},
			Then: AlertSpec{Text: "{item_goal} is late: the goal was {item_goal_due}. Farm efficiently and skip fights you don't need",
				Speech: "{item_goal} is late. Farm for it.", Severity: "warn", Mistake: true, Habit: "Late core items",
				Advice: "Your core items come late. Plan your farm around the next item and skip fights you don't need until it's done."},
			Cooldown: 300,
		},
		{
			ID: "farm_pace", Name: "Behind on last hits", Category: "economy", Roles: cores,
			When: Trigger{Type: WhenSchedule, First: 300, Every: 300, Until: 1800},
			If:   []Cond{{Field: "pace_diff", Op: "lt"}},
			Then: AlertSpec{Text: "{at}: {last_hits} last hits, target {lh_target}. Farm nearby camps between waves",
				Speech: "{last_hits} last hits. Target is {lh_target}.", Severity: "warn", Mistake: true,
				Habit:  "Behind on last hits",
				Advice: "You're behind on last hits. Practice last-hitting in the demo mode and farm nearby camps between waves."},
		},
		{
			ID: "farm_pace_good", Name: "On top of your last hits", Category: "economy", Roles: cores,
			When: Trigger{Type: WhenSchedule, First: 300, Every: 300, Until: 1800},
			If:   []Cond{{Field: "pace_diff", Op: "ge"}},
			Then: AlertSpec{Text: "{at}: {last_hits} last hits (target {lh_target}). Good farm, keep it up",
				Speech: "{last_hits} last hits. On track.", Severity: "info"},
		},
		{
			ID: "runes", Name: "Bounty rune spawns", Category: "timing",
			Roles: []string{dota.Mid, dota.SoftSupport, dota.HardSupport},
			When:  Trigger{Type: WhenSchedule, First: 0, Every: t.BountyRuneEvery, Lead: 15},
			Then:  AlertSpec{Text: "Bounty runes spawn in {in}s ({at})", Speech: "Bounty runes in {in} seconds", Severity: "info"},
		},
		{
			ID: "water_runes", Name: "Water rune spawns", Category: "timing",
			Roles: []string{dota.Mid, dota.SoftSupport, dota.HardSupport},
			When:  Trigger{Type: WhenSchedule, First: t.WaterRunes[0], Every: t.WaterRunes[0], Until: t.WaterRunes[len(t.WaterRunes)-1], Lead: 15},
			Then:  AlertSpec{Text: "Water runes spawn in {in}s ({at})", Speech: "Water runes in {in} seconds", Severity: "info"},
		},
		{
			ID: "power_runes", Name: "Power rune spawns while laning", Category: "timing",
			Roles: []string{dota.Mid},
			When:  Trigger{Type: WhenSchedule, First: t.PowerRuneFirst, Every: t.PowerRuneEvery, Until: laningEnds - 1, Lead: 15},
			Then:  AlertSpec{Text: "Power runes spawn in {in}s ({at})", Speech: "Power runes in {in} seconds", Severity: "info"},
		},
		{
			ID: "power_runes_near", Name: "Power rune spawning near you", Category: "timing",
			When: Trigger{Type: WhenSchedule, First: laningEnds, Every: t.PowerRuneEvery, Lead: 15},
			If: []Cond{
				{Field: "alive", Op: "true"},
				{Field: "power_rune_distance", Op: "le", Num: 3000},
			},
			Then: AlertSpec{Text: "Power rune spawns in {in}s at the {power_rune_side} river spot, close to you",
				Speech: "Power rune near you in {in} seconds", Severity: "info"},
		},
		{
			ID: "night", Name: "Night falls", Category: "timing",
			When: Trigger{Type: WhenSchedule, First: t.DayNightEvery, Every: 2 * t.DayNightEvery, Lead: 15},
			Then: AlertSpec{Text: "Night falls in {in}s ({at}). Vision shortens for both sides: ward, group up or back off",
				Speech: "Night in {in} seconds", Severity: "info"},
		},
		{
			ID: "wisdom_rune", Name: "Shrine of Wisdom activates", Category: "timing",
			Roles: []string{dota.Offlane, dota.SoftSupport, dota.HardSupport},
			When:  Trigger{Type: WhenSchedule, First: t.WisdomRuneEvery, Every: t.WisdomRuneEvery, Lead: 30},
			Then: AlertSpec{Text: "Shrines of Wisdom activate in {in}s ({at}). Stand in yours for 3 seconds; an enemy in it reverses the countdown",
				Speech: "Wisdom shrine in {in} seconds", Severity: "info"},
		},
		{
			ID: "stack", Name: "Camp stacking", Category: "timing", Roles: supports,
			When: Trigger{Type: WhenSchedule, First: 293, Every: 120, Until: 1793, Lead: 22},
			If:   []Cond{{Field: "alive", Op: "true"}},
			Then: AlertSpec{Text: "Stack a camp: pull at {at}", Speech: "Stack a camp", Severity: "info", Silent: true},
			Max:  6,
		},
		{
			ID: "neutral_tier", Name: "Neutral item tier unlocks", Category: "timing",
			When: Trigger{Type: WhenSchedule, First: t.NeutralTiers[0], Every: t.NeutralTiers[1] - t.NeutralTiers[0], Until: t.NeutralTiers[3]},
			Then: AlertSpec{Text: "Tier {neutral_tier} neutral items: craft yours with Madstone from cleared camps",
				Speech: "Tier {neutral_tier} neutral items", Severity: "info", Silent: true},
		},
		{
			ID: "neutral_tier_last", Name: "Last neutral item tier unlocks", Category: "timing",
			When: Trigger{Type: WhenSchedule, First: t.NeutralTiers[len(t.NeutralTiers)-1]},
			Then: AlertSpec{Text: "Tier {neutral_tier} neutral items: craft yours with Madstone from cleared camps",
				Speech: "Tier {neutral_tier} neutral items", Severity: "info"},
		},
		{
			ID: "tormentor", Name: "Tormentor spawn", Category: "timing",
			When: Trigger{Type: WhenSchedule, First: t.TormentorSpawn},
			Then: AlertSpec{Text: "Tormentors have spawned: a free Aghanim's Shard for whoever takes it",
				Speech: "Tormentor is up", Severity: "info"},
		},
		{
			ID: "no_tp", Name: "No TP scroll", Category: "survival",
			When: Trigger{Type: WhenState, For: 10},
			If: []Cond{
				{Field: "alive", Op: "true"},
				{Field: "clock", Op: "ge", Num: 60},
				{Field: "has_tp", Op: "false"},
				{Field: "gold", Op: "ge", Num: 100},
			},
			Then: AlertSpec{Text: "No TP scroll. Buy one ({tp_cost}g)", Speech: "Buy a TP scroll", Severity: "warn",
				Mistake: true, Advice: "Always carry a TP scroll. It saves towers, teammates and your own life."},
			Cooldown: 90, Max: 4,
		},
		{
			ID: "buyback", Name: "Buyback gold", Category: "survival", Roles: cores,
			When: Trigger{Type: WhenState, For: 20},
			If: []Cond{
				{Field: "alive", Op: "true"},
				{Field: "clock", Op: "ge", Num: float64(t.BuybackFrom)},
				{Field: "buyback_cooldown", Op: "eq"},
				{Field: "buyback_cost", Op: "gt"},
				{Field: "can_buyback", Op: "false"},
			},
			Then: AlertSpec{Text: "Save for buyback: {buyback_cost}g needed, you have {gold}", Speech: "Save gold for buyback",
				Severity: "warn", Mistake: true, Habit: "No buyback gold",
				Advice: "From 30 minutes on, keep enough gold to buy back. One buyback can decide the game."},
			Cooldown: 240, Max: 3,
		},
		{
			ID: "fountain_idle", Name: "Lingering in base at full HP", Category: "survival",
			When: Trigger{Type: WhenState, For: 12},
			If: []Cond{
				{Field: "alive", Op: "true"},
				{Field: "clock", Op: "gt"},
				{Field: "near_fountain", Op: "true"},
				{Field: "hp_pct", Op: "ge", Num: 95},
				{Field: "mana_pct", Op: "ge", Num: 90},
			},
			Then: AlertSpec{Text: "Full HP and mana. Leave base and get back on the map", Speech: "Leave the fountain",
				Severity: "warn", Mistake: true, Habit: "Idle in base at full HP",
				Advice: "Leave base as soon as you're healed. Time in base is time not farming or fighting."},
			Cooldown: 60, Max: 3,
		},
		{
			ID: "stash", Name: "Items left in stash", Category: "economy",
			When: Trigger{Type: WhenState, For: 15},
			If: []Cond{
				{Field: "alive", Op: "true"},
				{Field: "stash_items", Op: "ge", Num: 1},
			},
			Then: AlertSpec{Text: "Items are waiting in your stash. Send them with the courier", Speech: "Deliver your stash items",
				Severity: "warn", Mistake: true,
				Advice: "Items in the stash do nothing. Send the courier right after you buy."},
			Cooldown: 120, Max: 4,
		},
		{
			ID: "midas", Name: "Hand of Midas off cooldown", Category: "economy",
			When: Trigger{Type: WhenState, For: 8},
			If: []Cond{
				{Field: "alive", Op: "true"},
				{Field: "item_ready", Arg: "hand_of_midas", Op: "true"},
			},
			Then: AlertSpec{Text: "Hand of Midas is ready. Use it on a big creep", Speech: "Use Midas",
				Severity: "warn", Mistake: true, Habit: "Midas not used", Advice: "Use Hand of Midas every time it comes off cooldown."},
			Cooldown: 30, Max: 6,
		},
		{
			ID: "backpack", Name: "Active item stuck in backpack", Category: "items",
			When: Trigger{Type: WhenState, For: 20},
			If: []Cond{
				{Field: "alive", Op: "true"},
				{Field: "backpack_active", Op: "true"},
			},
			Then: AlertSpec{Text: "{backpack_item} is in your backpack. Swap it into your main inventory",
				Speech: "{backpack_item} is in your backpack", Severity: "warn", Mistake: true, Habit: "Active item in backpack",
				Advice: "Keep active items (wand, BKB, blink) in your main six slots."},
			Cooldown: 90, Max: 3,
		},
		{
			ID: "neutral_empty", Name: "Empty neutral item slot", Category: "items",
			When: Trigger{Type: WhenState, For: 30},
			If: []Cond{
				{Field: "alive", Op: "true"},
				{Field: "clock", Op: "ge", Num: float64(t.NeutralTiers[0] + 180)},
				{Field: "neutral_empty", Op: "true"},
			},
			Then: AlertSpec{Text: "Your neutral item slot is empty. Clear a camp for Madstone and craft a tier 1 item (5 Madstone)",
				Speech: "Your neutral slot is empty", Severity: "warn", Mistake: true, Habit: "Empty neutral slot",
				Advice: "Neutral items are free power. Craft one as soon as you have the Madstone for it."},
			Cooldown: 300, Max: 3,
		},
		{
			ID: "wards", Name: "Holding observer wards", Category: "items", Roles: supports,
			When: Trigger{Type: WhenState, For: 90},
			If: []Cond{
				{Field: "alive", Op: "true"},
				{Field: "holding_wards", Op: "true"},
			},
			Then: AlertSpec{Text: "You've been carrying observer wards. Place them", Speech: "Place your wards",
				Severity: "warn", Mistake: true, Habit: "Holding wards",
				Advice: "Wards in your inventory give no vision. Place them right after you buy them."},
			Cooldown: 90, Max: 4,
		},
		{
			ID: "low_hp", Name: "Low HP warning", Category: "survival",
			When: Trigger{Type: WhenState},
			If: []Cond{
				{Field: "alive", Op: "true"},
				{Field: "hp_pct", Op: "between", Num: 1, Num2: 25}, // at 0% it's too late
				{Field: "near_fountain", Op: "false"},
			},
			Then:     AlertSpec{Text: "Low HP ({hp_pct}%). {heal_advice}", Speech: "Low health. {heal_advice}", Severity: "urgent"},
			Cooldown: 25,
		},
		{
			ID: "unspent_gold", Name: "Too much unspent gold (cores)", Category: "economy", Roles: cores,
			When: Trigger{Type: WhenState, For: 30},
			If: []Cond{
				{Field: "alive", Op: "true"},
				{Field: "gold_beyond_next", Op: "ge", Num: 1200},
				{Field: "idle_income", Op: "ge", Num: 150},
			},
			Then: AlertSpec{Text: "{gold_spendable} gold unspent. Buy an item or its components",
				Speech: "Spend your gold", Severity: "warn", Mistake: true, Habit: "Unspent gold",
				Advice: "Gold in your pocket is lost when you die. Buy components as you go."},
			Cooldown: 180, Max: 5,
		},
		{
			ID: "unspent_gold_support", Name: "Too much unspent gold (supports)", Category: "economy", Roles: supports,
			When: Trigger{Type: WhenState, For: 30},
			If: []Cond{
				{Field: "alive", Op: "true"},
				{Field: "gold_beyond_next", Op: "ge", Num: 700},
				{Field: "idle_income", Op: "ge", Num: 180},
			},
			Then: AlertSpec{Text: "{gold_spendable} gold unspent. Buy wards, a courier upgrade or your next item",
				Speech: "Spend your gold", Severity: "warn", Mistake: true, Habit: "Unspent gold",
				Advice: "Supports win games with items on the map. Spend gold as it comes."},
			Cooldown: 180, Max: 5,
		},
		{
			ID: "next_item", Name: "Next build item affordable", Category: "economy",
			When: Trigger{Type: WhenChange},
			If: []Cond{
				{Field: "alive", Op: "true"},
				{Field: "next_item_ready", Op: "true"},
			},
			Then:     AlertSpec{Text: "You can afford {next_item}", Speech: "You can afford {next_item}", Severity: "info"},
			Cooldown: 300,
		},
		{
			ID: "idle", Name: "Standing around without farming", Category: "map", Roles: cores,
			When: Trigger{Type: WhenState},
			If: []Cond{
				{Field: "alive", Op: "true"},
				{Field: "idle_seconds", Op: "ge", Num: 30},
			},
			Then: AlertSpec{Text: "No farm or movement for {idle_seconds}s. Hit a camp, push a wave or join your team",
				Speech: "Don't stand still. Go farm.", Severity: "warn", Mistake: true, Habit: "Standing around",
				Advice: "Always be farming, pushing or fighting. Idle seconds add up to whole items."},
			Cooldown: 60, Max: 4,
		},
		{
			ID: "no_fight_gold", Name: "All farm, no fights", Category: "map", Roles: cores,
			When: Trigger{Type: WhenSchedule, First: 1500, Every: 600, Until: 3600},
			If: []Cond{
				{Field: "alive", Op: "true"},
				{Field: "earned_gold", Op: "ge", Num: 8000},
				{Field: "fight_gold_pct", Op: "le", Num: 8},
			},
			Then: AlertSpec{Text: "Only {fight_gold_pct}% of your gold came from kills. Group with your team and take a fight or an objective",
				Speech: "Almost none of your gold is from fights. Go with your team.", Severity: "info"},
		},
		{
			ID: "tower_defence", Name: "A building of yours is under attack", Category: "map",
			When: Trigger{Type: WhenChange},
			If: []Cond{
				{Field: "tower_falling", Op: "true"},
				{Field: "attacked_hp", Op: "le", Num: 70},
			},
			Then: AlertSpec{Text: "Your {tower_name} is under attack ({attacked_hp}% left). Defend it or TP in",
				Speech: "Your {tower_name} is under attack", Severity: "warn"},
			Cooldown: 90,
		},
		{
			ID: "glyph", Name: "Glyph a tower that's being pushed", Category: "map",
			When: Trigger{Type: WhenState},
			If: []Cond{
				{Field: "tower_drop", Op: "ge", Num: 15},
				{Field: "dropping_hp", Op: "le", Num: 50},
				{Field: "glyph_ready", Op: "true"},
			},
			Then: AlertSpec{Text: "Your {dropping_building} is dropping fast. Glyph it now",
				Speech: "Glyph your {dropping_building}", Severity: "urgent"},
			Cooldown: 300, Max: 5,
		},
		{
			ID: "glyph_refreshed", Name: "Glyph refreshed by a lost barracks", Category: "map",
			When: Trigger{Type: WhenChange}, Once: true,
			If: []Cond{{Field: "melee_rax_lost", Op: "ge", Num: 1}},
			Then: AlertSpec{Text: "You lost your first melee barracks, so your Glyph is ready again",
				Speech: "Your glyph is ready again", Severity: "warn"},
		},
		{
			ID: "buyback_defend", Name: "Buy back to defend your base", Category: "survival",
			When: Trigger{Type: WhenState, For: 2},
			If: []Cond{
				{Field: "alive", Op: "false"},
				{Field: "respawn", Op: "ge", Num: 20},
				{Field: "base_under_attack", Op: "true"},
				{Field: "can_buyback", Op: "true"},
			},
			Then: AlertSpec{Text: "Your {base_building} is under attack and you can buy back ({buyback_cost}g). Buy back to defend",
				Speech: "Buy back and defend", Severity: "urgent"},
			Cooldown: 60, Max: 3,
		},
		{
			ID: "wards_in_shop", Name: "Observer ward stock is full", Category: "items", Roles: supports,
			When: Trigger{Type: WhenState, For: 20},
			If: []Cond{
				{Field: "clock", Op: "gt"},
				{Field: "wards_in_shop", Op: "true"},
				{Field: "holding_wards", Op: "false"},
			},
			Then: AlertSpec{Text: "Observer ward stock is full, so restocks are going to waste. Buy wards and place them",
				Speech: "Ward stock is full", Severity: "info"},
			Cooldown: 240, Max: 5,
		},
		{
			ID: "skill_points", Name: "Unspent skill point", Category: "skills",
			When: Trigger{Type: WhenState, For: 15},
			If: []Cond{
				{Field: "skill_points", Op: "ge", Num: 1},
				// Everything worth a point is skilled by about level 22 since 7.40.
				{Field: "level", Op: "le", Num: 22},
			},
			Then: AlertSpec{Text: "You have an unspent skill point. {skill_tip} ({skill_source})", Speech: "{skill_tip}",
				Severity: "warn", Mistake: true,
				Advice: "Level up an ability as soon as you gain a level. An unspent point is wasted power."},
			Cooldown: 45, Max: 3,
		},
	}
	for i := range specs {
		specs[i].Match, specs[i].Enabled = "all", true
		specs[i] = translate(specs[i], lang)
	}
	return specs
}
