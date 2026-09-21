package dota

import "testing"

func TestPositions(t *testing.T) {
	for i, role := range Roles {
		if Position(role) != i+1 || RoleAt(i+1) != role {
			t.Errorf("%s: position %d, back to %q", role, Position(role), RoleAt(Position(role)))
		}
		if RoleName(role, "en") == "" || RoleName(role, "ru") == "" {
			t.Errorf("%s has no name", role)
		}
	}
	if Position("jungler") != 0 || RoleAt(0) != "" || RoleAt(6) != "" {
		t.Error("an unknown role or position should give nothing")
	}
}

func TestClock(t *testing.T) {
	for sec, want := range map[int]string{0: "0:00", 723: "12:03", -45: "-0:45", 3600: "60:00"} {
		if got := Clock(sec); got != want {
			t.Errorf("Clock(%d) = %s, want %s", sec, got, want)
		}
	}
}

// The item timing goals used their own median, which took the upper middle value; every
// target now uses this one.
func TestMedian(t *testing.T) {
	for _, c := range []struct {
		in   []int
		want int
	}{{nil, 0}, {[]int{7}, 7}, {[]int{3, 1, 2}, 2}, {[]int{600, 900}, 750}, {[]int{4, 1, 3, 2}, 2}} {
		if got := Median(c.in); got != c.want {
			t.Errorf("Median(%v) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestTalentsAtLevel(t *testing.T) {
	for level, want := range map[int]int{0: 0, 9: 0, 10: 1, 14: 1, 15: 2, 20: 3, 25: 4, 26: 4, 27: 5, 30: 8} {
		if got := TalentsAtLevel(level); got != want {
			t.Errorf("TalentsAtLevel(%d) = %d, want %d", level, got, want)
		}
	}
	if n := TalentsAtLevel(30); n != len(TalentLevels) {
		t.Errorf("level 30 gives %d talent points, but there are %d levels handing one out", n, len(TalentLevels))
	}
}

// TalentDueAt names the talent still waiting, so an alert can say which level it came from.
func TestTalentDueAt(t *testing.T) {
	for _, c := range []struct{ taken, level, want int }{
		{0, 9, 0},   // none handed out yet
		{0, 12, 10}, // the level 10 talent is waiting
		{1, 12, 0},  // and it has been taken
		{2, 21, 20},
		{4, 28, 27},
		{8, 30, 0}, // every talent taken
	} {
		if got := TalentDueAt(c.taken, c.level); got != c.want {
			t.Errorf("TalentDueAt(taken %d, level %d) = %d, want %d", c.taken, c.level, got, c.want)
		}
	}
}

func TestRankFloor(t *testing.T) {
	for tier, want := range map[int]int{11: 0, 15: 616, 21: 770, 51: 3080, 53: 3388, 65: 4466, 71: 4620, 75: 5420, 80: 5620} {
		if got, ok := RankFloor(tier); !ok || got != want {
			t.Errorf("RankFloor(%d) = %d/%v, want %d", tier, got, ok, want)
		}
	}
	for _, tier := range []int{0, 10, 16, 76, 81, 90} {
		if _, ok := RankFloor(tier); ok {
			t.Errorf("RankFloor(%d) took a number that isn't a rank", tier)
		}
	}
}

func TestRankTiersClimb(t *testing.T) {
	tiers := RankTiers()
	if len(tiers) != 36 || tiers[0] != 11 || tiers[len(tiers)-1] != 80 {
		t.Fatalf("RankTiers = %v, want Herald 1 to Immortal, five stars a medal", tiers)
	}
	prev := -1
	for _, tier := range tiers {
		floor, ok := RankFloor(tier)
		if !ok || floor <= prev {
			t.Errorf("rank %d starts at %d/%v, after %d", tier, floor, ok, prev)
		}
		prev = floor
	}
}

func TestRankName(t *testing.T) {
	for _, c := range []struct {
		tier       int
		lang, want string
	}{{53, "en", "Legend 3"}, {71, "ru", "Божество 1"}, {80, "en", "Immortal"}, {0, "en", ""}} {
		if got := RankName(c.tier, c.lang); got != c.want {
			t.Errorf("RankName(%d, %s) = %q, want %q", c.tier, c.lang, got, c.want)
		}
	}
}
