package server

import (
	"testing"
	"time"

	"gourdian/internal/dota"
	"gourdian/internal/model"
)

// The picking itself is internal/focus's own test; this is the wiring from the saved reviews
// to the engine.
func TestTheFocusReachesTheEngine(t *testing.T) {
	srv, _, _ := newTestServer(t, nil)
	r := model.Review{MatchID: "1", Hero: "Puck", HeroID: 13, Role: dota.Mid,
		NextGameFocus: "Hit 60 last hits by 10:00", Date: time.Now().Add(-time.Hour)}
	if err := srv.stats.AppendReview(r); err != nil {
		t.Fatal(err)
	}
	srv.applyFocus(dota.Mid, 13)
	if got := srv.engine.Snapshot(srv.cfg.Settings()).Focus; got != r.NextGameFocus {
		t.Fatalf("engine focus = %q, want %q", got, r.NextGameFocus)
	}
}
