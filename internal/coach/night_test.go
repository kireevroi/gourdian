package coach

import (
	"slices"
	"testing"

	"gourdian/internal/config"
	"gourdian/internal/dota"
)

// Vision shortens at night for both sides, so the trainer says it's coming.
func TestNightIsAnnouncedBeforeItFalls(t *testing.T) {
	got := byRule(play(newEngine(nil), settings(dota.HardSupport), 0, 1500, nil), "night")
	var at []int
	for _, tip := range got {
		at = append(at, tip.Clock)
	}
	if !slices.Equal(at, []int{285, 885, 1485}) {
		t.Fatalf("want a heads-up 15 seconds before 5:00, 15:00 and 25:00, got %v", at)
	}
	if got[0].Text != "Night falls in 15s (5:00). Vision shortens for both sides: ward, group up or back off" {
		t.Errorf("text = %q", got[0].Text)
	}
	if got[0].Speech != "Night in 15 seconds" {
		t.Errorf("speech = %q", got[0].Speech)
	}
}

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
