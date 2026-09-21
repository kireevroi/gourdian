package coach

import (
	"gourdian/internal/data/opendota"
	"gourdian/internal/game/dota"
	"gourdian/internal/game/gsi"
)

// SkillView is the ability pros level next, and where that order comes from.
type SkillView struct {
	Next     string   `json:"next,omitempty"`
	Points   int      `json:"points"`
	Source   string   `json:"source"`
	Games    int      `json:"games"`
	Position int      `json:"position,omitempty"` // 0 when the order is from every position
	Won      bool     `json:"won,omitempty"`
	Order    []string `json:"order"`
	// Done marks the points of the order the player's levels already cover; NextAt is the
	// point to take now, or -1.
	Done   []bool `json:"done"`
	NextAt int    `json:"next_at"`
}

func skillView(data Data, s *gsi.State, role, lang string, points int) *SkillView {
	b := data.SkillBuildFor(s.Hero.ID, role)
	if b == nil || len(b.Order) == 0 {
		return nil
	}
	v := &SkillView{Points: points, Source: skillSource(b, lang), Games: b.Games, Position: b.Position, Won: b.Won}
	v.NextAt = nextSkillAt(b.Order, s)
	if v.NextAt >= 0 {
		v.Next = data.AbilityName(b.Order[v.NextAt])
	}
	levels := map[string]int{}
	for _, a := range s.Abilities {
		levels[a.Name] = a.Level
	}
	taken := map[string]int{}
	for _, name := range b.Order {
		taken[name]++
		v.Order = append(v.Order, data.AbilityName(name))
		v.Done = append(v.Done, taken[name] <= levels[name])
	}
	return v
}

// nextSkill follows the player's own levels, so skilling differently earlier doesn't throw it off.
func nextSkill(order []string, s *gsi.State) string {
	if i := nextSkillAt(order, s); i >= 0 {
		return order[i]
	}
	return ""
}

func nextSkillAt(order []string, s *gsi.State) int {
	if s.Hero == nil {
		return -1
	}
	have := map[string]gsi.Ability{}
	for _, a := range s.Abilities {
		have[a.Name] = a
	}
	taken := map[string]int{}
	for i, name := range order {
		taken[name]++
		a, ok := have[name]
		if !ok || taken[name] <= a.Level {
			continue
		}
		if canLevel(a.Level+1, s.Hero.Level, a.Ultimate) {
			return i
		}
	}
	return -1
}

// canLevel: basic abilities take level n at hero level 2n-1, ultimates at 6, 12 and 18.
func canLevel(n, heroLevel int, ultimate bool) bool {
	if ultimate {
		return n <= 3 && heroLevel >= 6*n
	}
	return heroLevel >= 2*n-1
}

func (c *Ctx) skillBuild() *opendota.SkillBuild {
	if c.data == nil || c.S.Hero == nil {
		return nil
	}
	return c.data.SkillBuildFor(c.S.Hero.ID, c.Settings.Role)
}

// nextSkillName is how the game writes the ability pros level next, or "".
func (c *Ctx) nextSkillName() string {
	b := c.skillBuild()
	if b == nil {
		return ""
	}
	if name := nextSkill(b.Order, c.S); name != "" {
		return c.data.AbilityName(name)
	}
	return ""
}

// skillSource says which pro games the order comes from, such as "mid, 235 won pro games".
func skillSource(b *opendota.SkillBuild, lang string) string {
	if b == nil || len(b.Order) == 0 {
		return ""
	}
	w := sourceWords{lang}
	where := w.f("all positions", "все позиции")
	if b.Position > 0 {
		where = dota.RoleName(dota.RoleAt(b.Position), lang)
	}
	if b.Won {
		return w.f("%s, %d won pro games", "%s, побед про: %d", where, b.Games)
	}
	return w.f("%s, %d pro games", "%s, про-матчей: %d", where, b.Games)
}

// skillPoints counts unspent points against the fewest the hero has had, since innate abilities
// skew the raw count; a rise the player never spends becomes the new normal, not a warning.
type skillPoints struct {
	gapSet  bool
	gap     int
	seen    int
	spareAt int
	low     int
	lowSet  bool
	lowAt   int
}

func (p *skillPoints) see(s *gsi.State) {
	if s.Hero.Level == 0 || len(s.Abilities) == 0 {
		return
	}
	// Talents are not counted: they spend their own talent points, not skill points.
	spent := s.Hero.AttributesLevel
	for _, a := range s.Abilities {
		spent += a.Level
	}
	gap := SkillPointsAtLevel(s.Hero.Level) - spent
	clock := s.Map.ClockTime
	if gap >= p.gap || gap != p.low {
		p.lowSet = false
	}
	switch {
	case !p.gapSet:
		p.gapSet, p.gap, p.spareAt = true, gap, 0
	case gap < p.gap:
		// A level-up can show the new ability level one update before the new hero level.
		p.spareAt = 0
		if !p.lowSet {
			p.low, p.lowSet, p.lowAt = gap, true, clock
		} else if clock-p.lowAt >= skillSettle {
			p.gap, p.lowSet = gap, false
		}
	case gap > p.gap:
		if p.spareAt == 0 {
			p.spareAt = clock
		} else if clock-p.spareAt >= skillAccept {
			p.gap, p.spareAt = gap, 0
		}
	default:
		p.spareAt = 0
	}
	p.seen = gap
}

// spare is how many points are unspent beyond the hero's usual gap.
func (p *skillPoints) spare() int {
	if !p.gapSet {
		return 0
	}
	return max(p.seen-p.gap, 0)
}
