package server

import (
	"testing"
	"time"

	"gourdian/internal/dota"
	"gourdian/internal/stats"
)

func TestFocusFollowsPositionAndHero(t *testing.T) {
	srv, _, _ := newTestServer(t, nil)
	day := time.Now().Add(-72 * time.Hour)
	for i, r := range []stats.Review{
		{MatchID: "1", Hero: "Lion", HeroID: 26, Role: dota.HardSupport, NextGameFocus: "Stack the ancient camp every minute"},
		{MatchID: "2", Hero: "Puck", HeroID: 13, Role: dota.Mid, NextGameFocus: "Hit 60 last hits by 10:00"},
		{MatchID: "3", Hero: "Storm Spirit", HeroID: 17, Role: dota.Mid, NextGameFocus: "Leave lane with a bottle full"},
	} {
		r.Date = day.Add(time.Duration(i) * time.Hour)
		if err := srv.stats.AppendReview(r); err != nil {
			t.Fatal(err)
		}
	}
	set := srv.cfg.Settings()

	srv.applyFocus(dota.Mid, 13)
	if got := srv.engine.Snapshot(set).Focus; got != "Hit 60 last hits by 10:00" {
		t.Fatalf("mid on Puck got %q", got)
	}
	srv.applyFocus(dota.Mid, 99)
	if got := srv.engine.Snapshot(set).Focus; got != "Leave lane with a bottle full" {
		t.Fatalf("mid on a new hero got %q", got)
	}
	srv.applyFocus(dota.Carry, 99)
	if got := srv.engine.Snapshot(set).Focus; got != "Leave lane with a bottle full (from your Storm Spirit game as mid)" {
		t.Fatalf("carry with no carry review got %q", got)
	}
}
