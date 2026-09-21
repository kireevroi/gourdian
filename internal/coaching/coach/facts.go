package coach

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	"gourdian/internal/data/opendota"
	"gourdian/internal/game/gsi"
)

// MatchFacts is what the trainer has seen of this match beyond the snapshot, so the AI coach
// can talk about this game instead of guessing.
type MatchFacts struct {
	TeamKills, EnemyKills int
	Deaths                []int
	LostBuildings         []string
	UnderAttack           string
	Abilities             []AbilityFact
	SkillPoints           int
	HasAegis              bool
	Neutral               string
	Parts                 []PartsFact
}

type AbilityFact struct {
	Name     string
	Level    int
	Ready    bool
	Cooldown int
	Passive  bool
	Ultimate bool
}

// PartsFact is a build item the player already holds some parts of.
type PartsFact struct {
	Item    string
	Have    []string
	Missing []string
	Left    int
}

func (e *Engine) Facts(role string) MatchFacts {
	e.mu.Lock()
	defer e.mu.Unlock()
	var f MatchFacts
	s, m := e.last, e.match
	if s == nil || s.Map == nil || s.Hero == nil || s.Player == nil || m == nil {
		return f
	}
	f.TeamKills, f.EnemyKills = s.Map.RadiantScore, s.Map.DireScore
	if s.Player.TeamName == "dire" {
		f.TeamKills, f.EnemyKills = f.EnemyKills, f.TeamKills
	}
	f.Deaths = slices.Clone(m.deaths)
	for key, b := range s.Buildings[s.Player.TeamName] {
		if b.MaxHealth > 0 && b.Health <= 0 {
			f.LostBuildings = append(f.LostBuildings, buildingLabel(key))
		}
	}
	slices.Sort(f.LostBuildings)
	if key, drop := m.buildingDrop(nil); drop >= 5 {
		f.UnderAttack = fmt.Sprintf("%s (lost %d%% in the last 5s)", buildingLabel(key), drop)
	}
	f.Abilities = abilityFacts(s)
	f.SkillPoints = m.skillSpare()
	for _, it := range s.ItemsIn(gsi.Inventory, gsi.Backpack) {
		f.HasAegis = f.HasAegis || it.Short() == "aegis"
	}
	if e.data != nil {
		items := e.data.Items()
		if n, ok := s.Items["neutral0"]; ok && n.Name != "" && n.Name != "empty" {
			f.Neutral = itemDName(items, n.Short())
		}
		f.Parts = partsFacts(heldNames(s), items, e.data.BuildFor(s.Hero.ID, role), s.Map.ClockTime)
	}
	return f
}

func abilityFacts(s *gsi.State) []AbilityFact {
	prefix := strings.TrimPrefix(s.Hero.Name, "npc_dota_hero_") + "_"
	keys := make([]string, 0, len(s.Abilities))
	for k := range s.Abilities {
		keys = append(keys, k)
	}
	slices.SortFunc(keys, cmpKey)
	var out []AbilityFact
	for _, k := range keys {
		a := s.Abilities[k]
		if a.Name == "" || !strings.HasPrefix(a.Name, prefix) && a.Level == 0 || cosmetic.MatchString(a.Name) {
			continue
		}
		name := strings.ReplaceAll(strings.TrimPrefix(a.Name, prefix), "_", " ")
		out = append(out, AbilityFact{Name: titleCase(name), Level: a.Level, Ready: a.CanCast && a.Cooldown == 0,
			Cooldown: a.Cooldown, Passive: a.Passive, Ultimate: a.Ultimate})
	}
	return out
}

// cosmetic matches talents, hidden abilities and the ones every hero gets from events and Dota Plus.
var cosmetic = regexp.MustCompile(`^(special_bonus|plus_|seasonal_|ability_|twin_gate|portal_warp|high_five)|hidden|empty`)

// cmpKey orders "ability2" before "ability10".
func cmpKey(a, b string) int {
	if len(a) != len(b) {
		return len(a) - len(b)
	}
	return strings.Compare(a, b)
}

func titleCase(s string) string {
	words := strings.Fields(s)
	for i, w := range words {
		words[i] = strings.ToUpper(w[:1]) + w[1:]
	}
	return strings.Join(words, " ")
}

func itemDName(items map[string]opendota.ItemInfo, name string) string {
	if info, ok := items[name]; ok && info.DName != "" {
		return info.DName
	}
	return name
}

// partsFacts names the parts the player holds toward build items they haven't finished, and
// what's missing, so nobody has to guess a recipe.
func partsFacts(held []string, items map[string]opendota.ItemInfo, b *opendota.Build, clock int) []PartsFact {
	if b == nil || items == nil {
		return nil
	}
	p := b.Progress(held, items, clock)
	var out []PartsFact
	for i, it := range b.Items {
		if p.Owned[i] || len(items[it.Name].Components) == 0 {
			continue
		}
		counts := map[string]int{}
		for _, h := range held {
			counts[h]++
		}
		info := items[it.Name]
		f := PartsFact{Item: info.DName, Left: opendota.RemainingCost(it.Name, held, items)}
		recipe := info.Cost
		for _, c := range info.Components {
			recipe -= items[c].Cost
			if counts[c] > 0 {
				counts[c]--
				f.Have = append(f.Have, itemDName(items, c))
			} else {
				f.Missing = append(f.Missing, fmt.Sprintf("%s (%dg)", itemDName(items, c), items[c].Cost))
			}
		}
		if recipe > 0 {
			f.Missing = append(f.Missing, fmt.Sprintf("recipe (%dg)", recipe))
		}
		if len(f.Have) > 0 {
			out = append(out, f)
		}
	}
	return out
}
