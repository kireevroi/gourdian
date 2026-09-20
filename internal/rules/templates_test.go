package rules

import (
	"slices"
	"testing"

	"gourdian/internal/coach"
	"gourdian/internal/config"
	"gourdian/internal/dota"
	"gourdian/internal/gsi"
	"gourdian/internal/sim"
)

func template(t *testing.T, name string) coach.RuleSpec {
	t.Helper()
	i := slices.IndexFunc(Templates, func(s coach.RuleSpec) bool { return s.Name == name })
	if i < 0 {
		t.Fatalf("no %q template", name)
	}
	spec := Templates[i]
	spec.ID, spec.Enabled = "custom-"+name, true
	return spec
}

// played runs a template over a simulated match, the way "Run on recording" does on the
// dashboard, and returns the clocks it would have fired at.
func played(t *testing.T, spec coach.RuleSpec, role string, to int) []int {
	t.Helper()
	set := config.Default().Settings
	set.Role = role
	var at []int
	for _, tip := range coach.TestSpec(nil, spec, set, func(yield func(*gsi.State) bool) {
		for _, s := range sim.States(sim.Options{From: -60, To: to}) {
			if !yield(s) {
				return
			}
		}
	}) {
		at = append(at, tip.Clock)
	}
	return at
}

// Night falls at 5:00 and every ten minutes after it, and the template gives fifteen seconds'
// notice each time.
func TestNightTemplateWarnsBeforeEachNight(t *testing.T) {
	spec := template(t, "Night falls")
	if got := played(t, spec, dota.HardSupport, 1500); !slices.Equal(got, []int{285, 885, 1485}) {
		t.Fatalf("fired at %v, want 15 seconds before 5:00, 15:00 and 25:00", got)
	}
}
