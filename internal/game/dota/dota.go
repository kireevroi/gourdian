// Package dota is the game vocabulary the trainer shares across its packages: positions,
// lanes, clock times, map timings and the numbers personal targets are built from. It imports
// nothing of the trainer's, so any package can use it.
package dota

import (
	"fmt"
	"slices"
)

// The five positions, as settings and the match history spell them.
const (
	Carry       = "carry"
	Mid         = "mid"
	Offlane     = "offlane"
	SoftSupport = "soft_support"
	HardSupport = "hard_support"
)

// Roles are the positions in order: Roles[0] is position 1.
var Roles = []string{Carry, Mid, Offlane, SoftSupport, HardSupport}

func Position(role string) int { return slices.Index(Roles, role) + 1 }

func RoleAt(pos int) string {
	if pos < 1 || pos > len(Roles) {
		return ""
	}
	return Roles[pos-1]
}

func Core(role string) bool { return role == Carry || role == Mid || role == Offlane }

var roleNames = map[string][2]string{
	Carry:       {"carry", "керри"},
	Mid:         {"mid", "мид"},
	Offlane:     {"offlane", "оффлейн"},
	SoftSupport: {"soft support", "саппорт 4"},
	HardSupport: {"hard support", "хардсаппорт"},
}

func RoleName(role, lang string) string {
	if lang == "ru" {
		return roleNames[role][1]
	}
	return roleNames[role][0]
}

// Lanes, as lane detection and match reviews name them.
const (
	LaneSafe   = "safe lane"
	LaneMid    = "mid lane"
	LaneOff    = "offlane"
	LaneJungle = "jungle"
)

// LaneNumbered reads OpenDota's lane numbers: 1 safe, 2 mid, 3 off, 4 jungle.
func LaneNumbered(n int) string {
	if n < 1 || n > 4 {
		return ""
	}
	return []string{LaneSafe, LaneMid, LaneOff, LaneJungle}[n-1]
}

// Clock writes game seconds the way Dota's clock does: "12:03", and "-0:45" before the horn.
func Clock(sec int) string {
	sign := ""
	if sec < 0 {
		sign, sec = "-", -sec
	}
	return fmt.Sprintf("%s%d:%02d", sign, sec/60, sec%60)
}

func Median(values []int) int {
	if len(values) == 0 {
		return 0
	}
	s := slices.Clone(values)
	slices.Sort(s)
	if n := len(s); n%2 == 0 {
		return (s[n/2-1] + s[n/2]) / 2
	}
	return s[len(s)/2]
}

const (
	// PersonalGames is how many recent games on a hero in a position personal targets use.
	PersonalGames = 10
	GoalItemCost  = 2000
	// CoreItemCost is the least an item costs for its purchase time to be kept with a match.
	CoreItemCost = 1500
)

// Since 7.40 talents have points of their own, separate from the level's skill point.
var TalentLevels = []int{10, 15, 20, 25, 27, 28, 29, 30}

func TalentsAtLevel(level int) int {
	n := 0
	for _, l := range TalentLevels {
		if level >= l {
			n++
		}
	}
	return n
}

// TalentDueAt is the level of the next talent waiting to be taken, or 0 when none is.
func TalentDueAt(taken, level int) int {
	if taken < 0 || taken >= TalentsAtLevel(level) {
		return 0
	}
	return TalentLevels[taken]
}

// The Russian names follow the Russian client; index 0 is an unranked or unknown player.
var medalNames = [][2]string{
	{"", ""},
	{"Herald", "Рекрут"},
	{"Guardian", "Страж"},
	{"Crusader", "Рыцарь"},
	{"Archon", "Герой"},
	{"Legend", "Легенда"},
	{"Ancient", "Властелин"},
	{"Divine", "Божество"},
	{"Immortal", "Бессмертный"},
}

// Bracket reads an OpenDota rank tier, whose tens digit is the medal and ones digit the stars.
func Bracket(rankTier int) int {
	if b := rankTier / 10; b >= 1 && b < len(medalNames) {
		return b
	}
	return 0
}

func MedalName(bracket int, lang string) string {
	if bracket < 1 || bracket >= len(medalNames) {
		return ""
	}
	if lang == "ru" {
		return medalNames[bracket][1]
	}
	return medalNames[bracket][0]
}

const immortal = 80

func RankTiers() []int {
	var tiers []int
	for medal := 1; medal < immortal/10; medal++ {
		for star := 1; star <= 5; star++ {
			tiers = append(tiers, medal*10+star)
		}
	}
	return append(tiers, immortal)
}

// Valve doesn't publish rank thresholds; these are the ones players measured. The 7.41e squish
// only rescaled MMR inside Immortal, so none of them moved.
func RankFloor(tier int) (int, bool) {
	medal, star := tier/10, tier%10
	switch {
	case tier == immortal:
		return 5620, true
	case medal < 1 || medal >= immortal/10 || star < 1 || star > 5:
		return 0, false
	case medal == 7:
		return 4620 + (star-1)*200, true
	}
	return ((medal-1)*5 + star - 1) * 154, true
}

func RankName(tier int, lang string) string {
	name := MedalName(Bracket(tier), lang)
	if tier == immortal || name == "" {
		return name
	}
	return fmt.Sprintf("%s %d", name, tier%10)
}
