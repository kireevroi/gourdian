package coach

import (
	"slices"

	"gourdian/internal/config"
	"gourdian/internal/gsi"
)

// Lanes as the player's team sees them.
const (
	LaneSafe   = "safe lane"
	LaneMid    = "mid lane"
	LaneOff    = "offlane"
	LaneJungle = "jungle"
)

const (
	laneFrom     = 45  // skip walking out of base
	laneDecideAt = 150 // 2:30, before supports start rotating
	// laneUntil lets a trainer started mid-laning still decide, as long as it saw enough.
	laneUntil      = 300
	laneMinSamples = 60
	laneShare      = 0.6
	// Side lanes run along the map edges; mid runs along the diagonal between the fountains.
	laneEdge  = 4500
	laneWidth = 2200
)

// laneTracker counts, once per game second, which lane the hero stands in during laning.
type laneTracker struct {
	counts    map[string]int
	samples   int
	lastClock int
	wards     bool
	decided   bool
}

// laneAt classifies a map position for a team. Radiant's safe lane is the bottom one.
func laneAt(x, y int, team string) string {
	var side string
	switch {
	case abs(y-x) < laneWidth:
		return LaneMid
	case x < -laneEdge || y > laneEdge:
		side = "top"
	case x > laneEdge || y < -laneEdge:
		side = "bottom"
	default:
		return LaneJungle
	}
	if (side == "bottom") == (team == "radiant") {
		return LaneSafe
	}
	return LaneOff
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func (l *laneTracker) observe(s *gsi.State) {
	clock := s.Map.ClockTime
	if clock <= 120 {
		for _, it := range s.ItemsIn(gsi.Inventory, gsi.Backpack) {
			if n := it.Short(); n == "ward_observer" || n == "ward_sentry" || n == "ward_dispenser" {
				l.wards = true
			}
		}
	}
	h := s.Hero
	if clock < laneFrom || clock >= laneUntil || clock == l.lastClock || !h.Alive || h.XPos == 0 && h.YPos == 0 || nearFountain(s) {
		return
	}
	if l.counts == nil {
		l.counts = map[string]int{}
	}
	l.lastClock = clock
	l.counts[laneAt(h.XPos, h.YPos, s.Player.TeamName)]++
	l.samples++
}

// decide returns the lane the hero clearly laned in, once: at 2:30, or later if the trainer
// started late and needed more time to see enough of the laning phase.
func (l *laneTracker) decide(clock int) (string, bool) {
	if l.decided || clock < laneDecideAt || l.samples < laneMinSamples && clock < laneUntil {
		return "", false
	}
	l.decided = true
	if l.samples < laneMinSamples {
		return "", false
	}
	for lane, n := range l.counts {
		if lane != LaneJungle && float64(n) >= laneShare*float64(l.samples) {
			return lane, true
		}
	}
	return "", false
}

// roleForLane turns a lane into a position, using the current role and early wards to tell
// a core from the support sharing that lane.
func roleForLane(lane, current string, wards bool) string {
	support := wards || slices.Contains(supports, current)
	switch {
	case lane == LaneMid:
		return config.RoleMid
	case lane == LaneSafe && support:
		return config.RoleHardSupport
	case lane == LaneSafe:
		return config.RoleCarry
	case lane == LaneOff && support:
		return config.RoleSoftSupport
	case lane == LaneOff:
		return config.RoleOfflane
	}
	return ""
}
