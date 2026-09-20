package server

import (
	"testing"
	"time"

	"gourdian/internal/config"
	"gourdian/internal/dota"
	"gourdian/internal/gsi"
	"gourdian/internal/model"
)

func TestPickHelpUsesYourOwnRecord(t *testing.T) {
	srv, _, _ := newTestServer(t, nil)
	add := func(hero string, id int, role, result string, n int) {
		for i := range n {
			srv.stats.AppendMatch(model.MatchSummary{
				MatchID: hero + role + result + string(rune('a'+i)), Hero: hero, HeroID: id, Role: role,
				Result: result, Source: model.SourceLive, EndedAt: time.Now().Add(-time.Duration(i+1) * time.Hour),
				LastHitsAt: map[string]int{"10:00": 50},
			})
		}
	}
	add("Storm Spirit", 17, dota.Mid, "win", 4)
	add("Storm Spirit", 17, dota.Mid, "loss", 1)
	add("Invoker", 74, dota.Mid, "loss", 4)
	add("Invoker", 74, dota.Mid, "win", 1)
	add("Lion", 26, dota.HardSupport, "win", 3)
	add("Puck", 13, dota.Mid, "win", 1) // too few games to say anything

	p := srv.pickHelp(dota.Mid)
	if p == nil || len(p.Best) != 1 || p.Best[0].Hero != "Storm Spirit" || p.Best[0].WinPct != 80 {
		t.Fatalf("best = %+v", p)
	}
	if len(p.Avoid) != 1 || p.Avoid[0].Hero != "Invoker" || p.Avoid[0].WinPct != 20 {
		t.Fatalf("avoid = %+v", p.Avoid)
	}
	if p.Best[0].AvgLH10 != 50 {
		t.Fatalf("last hits = %d", p.Best[0].AvgLH10)
	}
	if other := srv.pickHelp(dota.Carry); other != nil {
		t.Fatalf("no carry games, so no help: %+v", other)
	}
}

// Pick help is for the draft: it used to wait for a match in progress without a hero, which
// Dota never sends, so it never showed.
func TestPickHelpShowsDuringTheDraft(t *testing.T) {
	srv, h, _ := newTestServer(t, func(s *config.Settings) { s.Role = dota.HardSupport })
	seed(t, srv, 10)
	draft := payload(-60, func(s *gsi.State) {
		s.Hero = &gsi.Hero{} // Dota's hero block before the pick: id 0
		s.Map.GameState = gsi.StateHeroSelection
	})
	postState(t, h, draft)
	if snap := srv.snapshot(srv.cfg.Settings()); snap.Picks == nil || len(snap.Picks.Best)+len(snap.Picks.Avoid) == 0 {
		t.Fatalf("no pick help during the draft: %+v", snap.Picks)
	}
	postState(t, h, payload(10, func(s *gsi.State) { s.Hero.ID = 26 })) // picked and playing
	if snap := srv.snapshot(srv.cfg.Settings()); snap.Picks != nil {
		t.Fatal("pick help still shown once the hero is picked")
	}
}
