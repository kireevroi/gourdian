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

// Position is a role's number, 1 (carry) to 5 (hard support), or 0 for anything else.
func Position(role string) int { return slices.Index(Roles, role) + 1 }

// RoleAt is the role at position 1 to 5, or "".
func RoleAt(pos int) string {
	if pos < 1 || pos > len(Roles) {
		return ""
	}
	return Roles[pos-1]
}

// Core reports whether a role is one of the farming positions.
func Core(role string) bool { return role == Carry || role == Mid || role == Offlane }

var roleNames = map[string][2]string{
	Carry:       {"carry", "керри"},
	Mid:         {"mid", "мид"},
	Offlane:     {"offlane", "оффлейн"},
	SoftSupport: {"soft support", "саппорт 4"},
	HardSupport: {"hard support", "хардсаппорт"},
}

// RoleName is how a role reads in a sentence, in English or Russian ("ru"); "" for an unknown role.
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

// LaneNumbered names OpenDota's lane numbers (1 safe, 2 mid, 3 off, 4 jungle), or "".
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

// Median is the middle value, or the mean of the two middle ones, and 0 for none. Every
// personal target uses it, so "your usual" means the same everywhere.
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
	// PersonalGames is how many of the player's latest games on a hero in a position their
	// personal targets come from.
	PersonalGames = 10
	// GoalItemCost is the least an item costs to be a core item timing goal.
	GoalItemCost = 2000
	// CoreItemCost is the least an item costs for its purchase time to be kept with a match.
	CoreItemCost = 1500
)

// TalentLevels are the levels that hand out a talent point, in order. Since 7.40 talents
// spend their own points rather than the level's skill point: one at 10, 15, 20 and 25, then
// one at every level from 27 to 30, which is enough to take both sides of every tier.
var TalentLevels = []int{10, 15, 20, 25, 27, 28, 29, 30}

// TalentsAtLevel is how many talents a hero may have taken by a level.
func TalentsAtLevel(level int) int {
	n := 0
	for _, l := range TalentLevels {
		if level >= l {
			n++
		}
	}
	return n
}

// TalentDueAt is the level whose talent is waiting when taken of them have been picked, or 0
// when none is: the talent after the ones already taken.
func TalentDueAt(taken, level int) int {
	if taken < 0 || taken >= TalentsAtLevel(level) {
		return 0
	}
	return TalentLevels[taken]
}

// medalNames are the ranked medals in order, English and Russian, following the Russian
// Dota client. Index 0 is an unranked or unknown player.
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

// Bracket is the skill bracket of an OpenDota rank tier (tens digit the medal, ones the
// stars): 1 for Herald up to 8 for Immortal, and 0 when the rank isn't known.
func Bracket(rankTier int) int {
	if b := rankTier / 10; b >= 1 && b < len(medalNames) {
		return b
	}
	return 0
}

// MedalName names a bracket from Bracket, in English or Russian ("ru"); "" when unknown.
func MedalName(bracket int, lang string) string {
	if bracket < 1 || bracket >= len(medalNames) {
		return ""
	}
	if lang == "ru" {
		return medalNames[bracket][1]
	}
	return medalNames[bracket][0]
}
