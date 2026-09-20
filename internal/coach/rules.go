package coach

import (
	"math"
	"strings"

	"gourdian/internal/dota"
	"gourdian/internal/gsi"
)

var (
	cores    = []string{dota.Carry, dota.Mid, dota.Offlane}
	supports = []string{dota.SoftSupport, dota.HardSupport}
)

// allRules compiles the rules that ship with the trainer, worded in lang.
func allRules(lang string) []Rule {
	var rules []Rule
	english := map[string]AlertSpec{}
	if lang != "en" {
		for _, spec := range DefaultSpecs("en") {
			english[spec.ID] = spec.Then
		}
	}
	for _, spec := range DefaultSpecs(lang) {
		r := compileSpec(spec)
		r.Custom = false
		if en, ok := english[spec.ID]; ok {
			r.english = &en
		}
		rules = append(rules, r)
	}
	return rules
}

func nextPeriodic(clock, first, period int) (int, bool) {
	if clock <= first {
		return first, true
	}
	if period <= 0 {
		return 0, false
	}
	return first + (clock-first+period-1)/period*period, true
}

// backpackActive is the first item in the backpack that does nothing there, or "".
func backpackActive(s *gsi.State) string {
	for _, it := range s.ItemsIn(gsi.Backpack) {
		if activeItems[it.Short()] {
			return it.Short()
		}
	}
	return ""
}

// teamSuffix names whose Roshan kill or Aegis an event was, when GSI reports the team.
// shardCost is what Aghanim's Shard costs, from OpenDota's item prices when they have loaded.
func shardCost(c *Ctx) float64 {
	if info, ok := c.items()["aghanims_shard"]; ok && info.Cost > 0 {
		return f64(info.Cost)
	}
	return dota.ShardCost
}

func teamSuffix(team, mine, preposition string) string {
	switch {
	case team == "" || mine == "":
		return ""
	case team == mine:
		return " " + preposition + " your team"
	default:
		return " " + preposition + " the enemy team"
	}
}

// Fountain coordinates shift between map versions, hence the generous radius.
func nearFountain(s *gsi.State) bool {
	h := s.Hero
	if h.XPos == 0 && h.YPos == 0 {
		return false
	}
	fx, fy := -7200.0, -6700.0
	if s.Player.TeamName == "dire" {
		fx, fy = 7200, 6600
	}
	return math.Hypot(float64(h.XPos)-fx, float64(h.YPos)-fy) < 1600
}

var healItems = []struct {
	name       string
	minCharges int
}{
	{"magic_wand", 5}, {"magic_stick", 5}, {"faerie_fire", 0}, {"cheese", 0}, {"guardian_greaves", 0}, {"mekansm", 0},
}

func heldNames(s *gsi.State) []string {
	var out []string
	for _, it := range s.ItemsIn(gsi.Inventory, gsi.Backpack, gsi.Stash) {
		out = append(out, it.Short())
	}
	return out
}

var activeItems = map[string]bool{}

func init() {
	for _, n := range strings.Fields(`magic_wand magic_stick black_king_bar blink overwhelming_blink swift_blink arcane_blink
		force_staff hurricane_pike glimmer_cape cyclone wind_waker sheepstick satanic manta ghost ethereal_blade lotus_orb
		pipe crimson_guard mekansm guardian_greaves arcane_boots hand_of_midas silver_edge invis_sword orchid bloodthorn
		nullifier abyssal_blade refresher shivas_guard blade_mail solar_crest spirit_vessel urn_of_shadows rod_of_atos gungir
		diffusal_blade disperser heavens_halberd bfury harpoon boots_of_bearing holy_locket ancient_janggo veil_of_discord
		meteor_hammer helm_of_the_overlord helm_of_the_dominator mjollnir armlet mask_of_madness phase_boots power_treads
		bottle faerie_fire cheese travel_boots travel_boots_2 soul_ring pavise dagon dagon_2 dagon_3 dagon_4 dagon_5 sphere
		bullwhip`) {
		activeItems[n] = true
	}
}

// SkillPointsAtLevel is how many skill points a hero has had by a level. Since 7.40 talents
// have their own points, so every level gives one skill point (checked on a 7.41 match).
func SkillPointsAtLevel(level int) int { return level }
