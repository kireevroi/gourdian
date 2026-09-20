package server

import (
	"slices"
	"strings"
	"testing"
	"time"

	"gourdian/internal/coach"
	"gourdian/internal/config"
	"gourdian/internal/dota"
	"gourdian/internal/gsi"
	"gourdian/internal/model"
)

func TestPickBoardUsesYourOwnRecord(t *testing.T) {
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

	p := srv.pickBoard(roleSet(srv, dota.Mid))
	if p == nil || len(p.Best) != 1 || p.Best[0].Name != "Storm Spirit" || p.Best[0].WinPct != 80 {
		t.Fatalf("best = %+v", p)
	}
	if len(p.Avoid) != 1 || p.Avoid[0].Name != "Invoker" || p.Avoid[0].WinPct != 20 {
		t.Fatalf("avoid = %+v", p.Avoid)
	}
	if p.Best[0].AvgLH10 != 50 {
		t.Fatalf("last hits = %d", p.Best[0].AvgLH10)
	}
	if other := srv.pickBoard(roleSet(srv, dota.Carry)); other != nil {
		t.Fatalf("no carry games, so no help: %+v", other)
	}
}

// roleSet is the player's settings with the position swapped, for asking about another one.
func roleSet(srv *Server, role string) config.Settings {
	set := srv.cfg.Settings()
	set.Role = role
	return set
}

// Pick help is for the draft: it used to wait for a match in progress without a hero, which
// Dota never sends, so it never showed.
func TestPickBoardShowsDuringTheDraft(t *testing.T) {
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

// The board is read out once as the draft opens, not twice a second for the whole draft.
func TestPickBoardIsSpokenOnceADraft(t *testing.T) {
	srv, h, _ := newTestServer(t, func(s *config.Settings) { s.Role = dota.Mid })
	for i := range 5 {
		srv.stats.AppendMatch(model.MatchSummary{
			MatchID: "storm" + string(rune('a'+i)), Hero: "Storm Spirit", HeroID: 17, Role: dota.Mid,
			Result: "win", Source: model.SourceLive, EndedAt: time.Now().Add(-time.Duration(i+1) * time.Hour)})
	}
	draft := func(clock int) *gsi.State {
		return payload(clock, func(s *gsi.State) {
			s.Hero = &gsi.Hero{} // Dota's hero block before the pick: id 0
			s.Map.GameState = gsi.StateHeroSelection
		})
	}
	postState(t, h, draft(-90))
	postState(t, h, draft(-89))
	spoken := picksSpoken(srv)
	if len(spoken) != 1 {
		t.Fatalf("picks spoken %d times, want once: %+v", len(spoken), spoken)
	}
	if !strings.Contains(spoken[0].Text, "Storm Spirit") {
		t.Errorf("spoken picks = %q", spoken[0].Text)
	}

	// Picking a hero ends the draft, and starting the match clears the tip list.
	postState(t, h, payload(10, func(s *gsi.State) { s.Hero.ID = 17 }))
	if got := picksSpoken(srv); len(got) != 0 {
		t.Fatalf("starting a match left %d pick tips behind", len(got))
	}
	// So the next draft speaking again shows up as one fresh tip.
	postState(t, h, draft(-90))
	if got := picksSpoken(srv); len(got) != 1 {
		t.Fatalf("the next draft spoke %d times, want once", len(got))
	}
}

// picksSpoken is the pick tips the trainer has read out in this match.
func picksSpoken(srv *Server) []coach.Tip {
	return slices.DeleteFunc(srv.engine.RecentTips(), func(t coach.Tip) bool { return t.Rule != "picks" })
}
