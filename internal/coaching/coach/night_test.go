package coach

import (
	"slices"
	"testing"

	"gourdian/internal/game/dota"
	"gourdian/internal/sys/config"
)

func timerLabels(clock int, daytime bool) []string {
	set := config.Default().Settings
	set.Role = dota.Carry
	var out []string
	for _, tm := range timers(clock, daytime, set, nil) {
		if tm.Kind == "daynight" {
			out = append(out, tm.Label)
		}
	}
	return out
}

// The turn shows up as it comes close, and is named by what the game says it is now, so Night
// Stalker's ultimate doesn't make the HUD lie.
func TestDayNightTimerAppearsNearTheTurn(t *testing.T) {
	for _, c := range []struct {
		name    string
		clock   int
		daytime bool
		want    []string
	}{
		{"well before the turn", 120, true, nil},
		{"a minute before night", 245, true, []string{"Night falls"}},
		{"just before night", 299, true, []string{"Night falls"}},
		{"a minute before day", 545, false, []string{"Day breaks"}},
		{"before the horn", -30, true, nil},
		{"night brought on early", 250, false, []string{"Day breaks"}},
	} {
		if got := timerLabels(c.clock, c.daytime); !slices.Equal(got, c.want) {
			t.Errorf("%s: timers = %v, want %v", c.name, got, c.want)
		}
	}
}

// The HUD's timer list is sorted by how soon each one is, and the turn takes its place in it.
func TestDayNightTimerIsSortedWithTheRest(t *testing.T) {
	set := config.Default().Settings
	set.Role = dota.Carry
	var last int
	found := false
	for i, tm := range timers(280, true, set, nil) {
		if i > 0 && tm.At < last {
			t.Fatalf("timers are out of order at %+v", tm)
		}
		last = tm.At
		found = found || tm.Kind == "daynight"
	}
	if !found {
		t.Error("no day or night timer 20 seconds before the turn")
	}
}
