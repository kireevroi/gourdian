package sim_test

import (
	"io"
	"log/slog"
	"testing"

	"gourdian/internal/coach"
	"gourdian/internal/config"
	"gourdian/internal/dota"
	"gourdian/internal/sim"
)

// coached runs a simulated match through the trainer and returns the tips one rule gave.
func coached(rule string) []coach.Tip {
	set := config.Default().Settings
	set.Role = dota.Carry
	e := coach.New(nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	var out []coach.Tip
	for _, s := range sim.States(sim.Options{From: -60, To: 2400}) {
		for _, tip := range e.Update(s, set).Tips {
			if tip.Rule == rule {
				out = append(out, tip)
			}
		}
	}
	return out
}

// The scripted player leaves the level 6 skill point unspent for half a minute, and nothing
// else: every later level goes into an ability or the attribute bonus, the way a real hero's
// does. Talents have their own points since 7.40, so they must not look like skill points.
func TestSimulatedPlayerOnlyMissesTheSkillPointOnPurpose(t *testing.T) {
	got := coached("skill_points")
	if len(got) != 1 || got[0].Clock != 390 {
		t.Fatalf("want one unspent skill point at 6:30, got %+v", got)
	}
}

// And it leaves the level 10 talent for a moment, so that reminder can be seen too.
func TestSimulatedPlayerForgetsOneTalent(t *testing.T) {
	got := coached("talent")
	if len(got) != 1 || got[0].Text != "Your level 10 talent is unspent. Take one from the talent tree" {
		t.Fatalf("want one level 10 talent reminder, got %+v", got)
	}
}
