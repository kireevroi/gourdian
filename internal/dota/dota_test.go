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
