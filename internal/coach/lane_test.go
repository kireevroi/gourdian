package coach

import (
	"testing"

	"dotatrainer/internal/config"
	"dotatrainer/internal/gsi"
)

func TestLaneAt(t *testing.T) {
	cases := []struct {
		x, y int
		team string
		want string
	}{
		{-334, 75, "radiant", LaneMid},
		{4900, -6100, "radiant", LaneSafe},
		{-6200, 1800, "radiant", LaneOff},
		{-4700, 6000, "dire", LaneSafe},
		{6200, -1600, "dire", LaneOff},
		{-3000, 2500, "radiant", LaneJungle},
	}
	for _, c := range cases {
		if got := laneAt(c.x, c.y, c.team); got != c.want {
			t.Errorf("laneAt(%d, %d, %s) = %s, want %s", c.x, c.y, c.team, got, c.want)
		}
	}
}

func TestRoleForLane(t *testing.T) {
	cases := []struct {
		lane, current string
		wards         bool
		want          string
	}{
		{LaneMid, config.RoleSoftSupport, true, config.RoleMid},
		{LaneSafe, config.RoleMid, false, config.RoleCarry},
		{LaneSafe, config.RoleCarry, true, config.RoleHardSupport},
		{LaneOff, config.RoleHardSupport, false, config.RoleSoftSupport},
		{LaneOff, config.RoleCarry, false, config.RoleOfflane},
		{LaneJungle, config.RoleCarry, false, ""},
	}
	for _, c := range cases {
		if got := roleForLane(c.lane, c.current, c.wards); got != c.want {
			t.Errorf("roleForLane(%s, %s, %v) = %q, want %q", c.lane, c.current, c.wards, got, c.want)
		}
	}
}

func detectOnce(t *testing.T, from, to int, mutate func(*gsi.State)) (Result, int) {
	t.Helper()
	e := newEngine(nil)
	set := settings(config.RoleSoftSupport)
	var found Result
	count := 0
	for clock := from; clock <= to; clock++ {
		s := state(clock)
		s.Hero.XPos, s.Hero.YPos = -300, 100
		if mutate != nil {
			mutate(s)
		}
		if res := e.Update(s, set); res.DetectedRole != "" {
			found = res
			count++
		}
	}
	return found, count
}

func TestLaningMidSwitchesToMidOnce(t *testing.T) {
	res, n := detectOnce(t, 0, 400, nil)
	if n != 1 || res.DetectedRole != config.RoleMid || res.DetectedLane != LaneMid {
		t.Fatalf("detected %d times: %+v", n, res)
	}
}

func TestLateStartStillDetectsBeforeFiveMinutes(t *testing.T) {
	if res, n := detectOnce(t, 170, 400, nil); n != 1 || res.DetectedRole != config.RoleMid {
		t.Fatalf("detected %d times: %+v", n, res)
	}
	if _, n := detectOnce(t, 260, 400, nil); n != 0 {
		t.Fatal("too little laning seen to decide")
	}
}

func TestNoDetectionWhenRoleAlreadyFits(t *testing.T) {
	if _, n := detectOnce(t, 0, 400, func(s *gsi.State) { s.Hero.XPos, s.Hero.YPos = -6200, 1800 }); n != 0 {
		t.Fatal("a soft support in the radiant offlane is already coached right")
	}
}
